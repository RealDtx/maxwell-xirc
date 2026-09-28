package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/library"
)

type fileEntry struct {
	Name     string    `json:"name"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"` // files only
	Modified time.Time `json:"modified"`
	Items    int       `json:"items,omitempty"` // dirs only: number of visible entries
}

type filesResponse struct {
	Dir     string      `json:"dir"`
	Root    string      `json:"root"`   // outermost destination containing dir (breadcrumb start)
	Parent  string      `json:"parent"` // "" when dir is a root
	Entries []fileEntry `json:"entries"`
}

// /api/files — file manager limited to folders at or below configured roots.
//
//	GET  (no dir)                                   {"roots": [...]}
//	GET  ?dir=ABS                                   list dir
//	POST {action:"move",   src_dir, names, dest_dir} move files/folders
//	POST {action:"delete", dir, names}               delete (folders recursively)
//	POST {action:"rename", dir, name, new_name}
//	POST {action:"mkdir",  dir, name}
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListFiles(w, r)
	case http.MethodPost:
		s.handleFileAction(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// fileRoots returns the destination roots, symlinks resolved.
func (s *Server) fileRoots() []string {
	var roots []string
	for _, d := range s.configuredRoots() {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			roots = append(roots, r)
		}
	}
	return roots
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		roots := s.advertisedRoots()
		sort.Strings(roots)
		writeJSON(w, http.StatusOK, map[string][]string{"roots": roots})
		return
	}
	clean := filepath.Clean(dir)
	if ok, err := s.isValidTargetDir(clean); err != nil || !ok {
		writeError(w, http.StatusForbidden, "not a configured destination directory")
		return
	}

	infos, err := os.ReadDir(clean)
	if err != nil {
		if fscheck.IsPermission(err) {
			writeError(w, http.StatusForbidden, fscheck.Describe(err, clean).Error())
		} else {
			writeError(w, http.StatusNotFound, "directory not found")
		}
		return
	}

	resp := filesResponse{Dir: clean, Entries: []fileEntry{}}
	if parent := filepath.Dir(clean); parent != clean {
		if ok, _ := s.isValidTargetDir(parent); ok {
			resp.Parent = parent
		}
	}
	resp.Root = clean
	real, _ := filepath.EvalSymlinks(clean)
	for _, root := range s.fileRoots() {
		if within(root, real) && len(root) < len(resp.Root) {
			// Report the root in the caller's (unresolved) spelling.
			if rel, err := filepath.Rel(root, real); err == nil {
				resp.Root = filepath.Clean(strings.TrimSuffix(clean, rel))
			}
		}
	}

	for _, e := range infos {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fe := fileEntry{Name: e.Name(), IsDir: info.IsDir(), Modified: info.ModTime().UTC()}
		if fe.IsDir {
			if sub, err := os.ReadDir(filepath.Join(clean, e.Name())); err == nil {
				for _, x := range sub {
					if !strings.HasPrefix(x.Name(), ".") {
						fe.Items++
					}
				}
			}
		} else {
			fe.Size = info.Size()
		}
		resp.Entries = append(resp.Entries, fe)
	}
	sort.Slice(resp.Entries, func(i, j int) bool {
		a, b := resp.Entries[i], resp.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	writeJSON(w, http.StatusOK, resp)
}

type fileActionRequest struct {
	Action  string   `json:"action"`
	Dir     string   `json:"dir"`
	SrcDir  string   `json:"src_dir"`
	DestDir string   `json:"dest_dir"`
	Name    string   `json:"name"`
	NewName string   `json:"new_name"`
	Names   []string `json:"names"`

	DeleteArchive bool `json:"delete_archive"`
}

// errNotAllowed marks client errors that map to 4xx per-item messages.
var errNotAllowed = errors.New("not allowed")

func validName(name string) bool {
	return name != "" && name == filepath.Base(name) && name != "." && name != ".." &&
		!strings.HasPrefix(name, ".") && !strings.ContainsAny(name, "\x00/\\")
}

func (s *Server) handleFileAction(w http.ResponseWriter, r *http.Request) {
	var req fileActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	roots := s.fileRoots()
	checkDir := func(d string) (string, bool) {
		d = filepath.Clean(d)
		ok, err := s.isValidTargetDir(d)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "not a configured destination directory: "+d)
			return "", false
		}
		return d, true
	}

	switch req.Action {
	case "move":
		src, ok := checkDir(req.SrcDir)
		if !ok {
			return
		}
		dest, ok := checkDir(req.DestDir)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, eachItem(req.Names, func(name string) error {
			return moveItem(roots, src, name, dest)
		}))
	case "delete":
		dir, ok := checkDir(req.Dir)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, eachItem(req.Names, func(name string) error {
			p, err := existingItem(roots, dir, name)
			if err != nil {
				return err
			}
			return fscheck.Describe(os.RemoveAll(p), dir) // a symlink is removed itself, never its target
		}))
	case "rename":
		dir, ok := checkDir(req.Dir)
		if !ok {
			return
		}
		src, err := existingItem(roots, dir, req.Name)
		if err == nil && !validName(req.NewName) {
			err = fmt.Errorf("%w: invalid new name", errNotAllowed)
		}
		dst := filepath.Join(dir, req.NewName)
		if err == nil {
			err = noClobber(dst)
		}
		if err == nil {
			err = fscheck.Describe(os.Rename(src, dst), dir)
		}
		if err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": dst})
	case "mkdir":
		dir, ok := checkDir(req.Dir)
		if !ok {
			return
		}
		if !validName(req.Name) {
			writeError(w, http.StatusBadRequest, "invalid folder name")
			return
		}
		p := filepath.Join(dir, req.Name)
		if err := os.Mkdir(p, 0775); err != nil {
			switch {
			case os.IsExist(err):
				writeError(w, http.StatusConflict, "already exists")
			case fscheck.IsPermission(err):
				writeError(w, http.StatusForbidden, fscheck.Describe(err, dir).Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": p})
	case "extract":
		dir, ok := checkDir(req.Dir)
		if !ok {
			return
		}
		if _, err := existingItem(roots, dir, req.Name); err != nil {
			writeError(w, statusFor(err), err.Error())
			return
		}
		set, isArchive := library.ArchiveVolumes(dir, req.Name)
		if !isArchive {
			writeError(w, http.StatusBadRequest, "not an archive")
			return
		}
		if !set.First {
			writeError(w, http.StatusBadRequest, "not the first volume of the archive set")
			return
		}
		target := filepath.Join(dir, set.Stem)
		if _, busy := s.extracting.LoadOrStore(target, struct{}{}); busy {
			writeError(w, http.StatusConflict, "extraction already running")
			return
		}
		if err := noClobber(target); err != nil {
			s.extracting.Delete(target)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		go s.runExtract(dir, req.Name, target, set.Volumes, req.DeleteArchive)
		writeJSON(w, http.StatusAccepted, map[string]string{"target": target})
	default:
		writeError(w, http.StatusBadRequest, "unknown action")
	}
}

// runExtract unpacks dir/name into target (via a staging dir in dir, then a
// rename) and deletes the archive's volumes only if that fully succeeded.
func (s *Server) runExtract(dir, name, target string, volumes []string, deleteArchive bool) {
	defer s.extracting.Delete(target)
	_, staging, err := library.ExtractAny(filepath.Join(dir, name), dir)
	if err == nil {
		if err = noClobber(target); err == nil {
			err = os.Rename(staging, target)
		}
		if err != nil {
			os.RemoveAll(staging)
		}
	}
	if err == nil && deleteArchive {
		for _, v := range volumes {
			if rmErr := os.Remove(filepath.Join(dir, v)); rmErr != nil {
				log.Printf("extract: removing %s: %v", v, rmErr)
			}
		}
	}
	data := map[string]interface{}{"dir": dir, "name": name, "target": target, "ok": err == nil, "error": ""}
	if err != nil {
		log.Printf("extract %s failed: %v", filepath.Join(dir, name), err)
		data["error"] = fscheck.Describe(err, dir).Error()
	}
	if s.ircMgr != nil {
		s.ircMgr.EventBus().Publish(irc.Event{Type: irc.EventFileExtract, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Data: data})
	}
}

// eachItem applies fn to every name and reports per-item results.
func eachItem(names []string, fn func(string) error) map[string]interface{} {
	done := []string{}
	errs := map[string]string{}
	for _, n := range names {
		if err := fn(n); err != nil {
			errs[n] = err.Error()
		} else {
			done = append(done, n)
		}
	}
	return map[string]interface{}{"done": done, "errors": errs}
}

// existingItem returns dir/name if it exists (not following a final symlink)
// and is neither a destination root nor an ancestor of one.
func existingItem(roots []string, dir, name string) (string, error) {
	if !validName(name) {
		return "", fmt.Errorf("%w: invalid name", errNotAllowed)
	}
	p := filepath.Join(dir, name)
	info, err := os.Lstat(p)
	if err != nil {
		return "", os.ErrNotExist
	}
	if info.IsDir() {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", err
		}
		for _, root := range roots {
			if within(real, root) {
				return "", fmt.Errorf("%w: %s is (or contains) a destination root", errNotAllowed, name)
			}
		}
	}
	return p, nil
}

func noClobber(p string) error {
	if _, err := os.Lstat(p); err == nil {
		return fmt.Errorf("%w: %s already exists", os.ErrExist, filepath.Base(p))
	}
	return nil
}

func moveItem(roots []string, srcDir, name, destDir string) error {
	src, err := existingItem(roots, srcDir, name)
	if err != nil {
		return err
	}
	if filepath.Clean(srcDir) == filepath.Clean(destDir) {
		return fmt.Errorf("%w: already in that folder", errNotAllowed)
	}
	realSrc, _ := filepath.EvalSymlinks(src)
	realDest, _ := filepath.EvalSymlinks(destDir)
	if within(realSrc, realDest) {
		return fmt.Errorf("%w: cannot move a folder into itself", errNotAllowed)
	}
	dst := filepath.Join(destDir, name)
	if err := noClobber(dst); err != nil {
		return err
	}
	// ponytail: rename only; destinations share one filesystem (checked on the Pi).
	// Add copy+delete for cross-device moves if that ever changes.
	err = os.Rename(src, dst)
	if err != nil && fscheck.IsPermission(err) && !fscheck.Probe(srcDir).Write {
		return fscheck.Describe(err, srcDir)
	}
	return fscheck.Describe(err, destDir)
}

// Types the OS mime database often lacks; checked before mime.TypeByExtension.
var rawMimeFallback = map[string]string{
	".mkv": "video/x-matroska", ".webm": "video/webm", ".mp4": "video/mp4", ".m4v": "video/mp4",
	".mov": "video/quicktime", ".flac": "audio/flac", ".m4a": "audio/mp4", ".opus": "audio/ogg",
	".ogg": "audio/ogg", ".mp3": "audio/mpeg", ".pdf": "application/pdf",
}

// GET /api/files/raw?path=ABS[&download=1] — stream one file below a
// configured root (Range/HEAD/If-Modified-Since via http.ServeContent).
func (s *Server) handleFilesRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := filepath.Clean(r.URL.Query().Get("path"))
	if !filepath.IsAbs(p) {
		writeError(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	if ok, err := s.isValidTargetDir(filepath.Dir(p)); err != nil || !ok {
		writeError(w, http.StatusForbidden, "not below a configured destination directory")
		return
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	inRoot := false
	for _, root := range s.fileRoots() {
		if within(root, real) {
			inRoot = true
			break
		}
	}
	if !inRoot {
		writeError(w, http.StatusForbidden, "not below a configured destination directory")
		return
	}
	f, err := os.Open(real)
	if err != nil {
		writeError(w, statusFor(fscheck.Describe(err, filepath.Dir(p))), "cannot open file")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "not a regular file")
		return
	}
	name := filepath.Base(p)
	ext := strings.ToLower(filepath.Ext(name))
	ct := rawMimeFallback[ext]
	if ct == "" {
		ct = mime.TypeByExtension(ext)
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	disp := "inline"
	if r.URL.Query().Get("download") == "1" {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func statusFor(err error) int {
	switch {
	case fscheck.IsPermission(err):
		return http.StatusForbidden
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, os.ErrExist):
		return http.StatusConflict
	case errors.Is(err, errNotAllowed):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
