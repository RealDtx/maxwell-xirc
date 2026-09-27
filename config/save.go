package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveKeys writes the keys of v (anything that encodes to a YAML mapping)
// into the config file at path, keeping comments and every other key. Nested
// mappings merge; any other value replaces the old one. A missing file is
// created (0640). The write is atomic: temp file in the same dir, then rename.
func SaveKeys(path string, v any) error {
	var doc yaml.Node
	mode := os.FileMode(0640)
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return fmt.Errorf("reading config: %w", err)
	default:
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("config: top level is not a mapping")
	}
	var src yaml.Node
	if err := src.Encode(v); err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}
	mergeMapping(root, &src)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	enc.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}

// mergeMapping sets every key of src into dst. Mappings on both sides merge
// recursively; otherwise src's value replaces dst's, keeping dst's trailing
// comment.
func mergeMapping(dst, src *yaml.Node) {
	for i := 0; i+1 < len(src.Content); i += 2 {
		k, v := src.Content[i], src.Content[i+1]
		j := -1
		for n := 0; n+1 < len(dst.Content); n += 2 {
			if dst.Content[n].Value == k.Value {
				j = n
				break
			}
		}
		switch {
		case j < 0:
			dst.Content = append(dst.Content, k, v)
		case v.Kind == yaml.MappingNode && dst.Content[j+1].Kind == yaml.MappingNode:
			mergeMapping(dst.Content[j+1], v)
		default:
			v.LineComment = dst.Content[j+1].LineComment
			dst.Content[j+1] = v
		}
	}
}
