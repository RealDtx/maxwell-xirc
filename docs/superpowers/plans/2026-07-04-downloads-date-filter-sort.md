# Downloads Date/Filter/Sort Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a visible "Date" column, per-column sorting, and a text filter to the Downloads view, defaulting to date-descending (newest first).

**Architecture:** Frontend-only change in the vanilla-JS/Alpine.js app (`web/js/app.js`, `web/index.html`). No backend/API changes — `created_at`, `filename`, `bot_nick`, `filesize`, `speed`/`average_speed`, and progress inputs are already returned by `getDownloads()`. Mirrors the existing `searchSort`/`sortBy`/`sortIndicator`/comparator pattern already used for the search-results table (`web/js/app.js:58,893-931`), applied to a new independent `dlSort` state so it doesn't interfere with search sorting.

**Tech Stack:** Vanilla JS, Alpine.js, no build step, no bundler, no JS test framework present in this repo.

## Global Constraints

- No backend/API/DB changes — this is a client-side-only rendering/filtering change.
- Reuse `fmtDate` (`web/js/app.js:1342`) for date formatting — do not add a new date-formatting helper.
- Reuse the comparator style from `displaySearchRows()` (`web/js/app.js:906-931`) — numeric subtraction when both values are numbers, else lowercase string compare — do not introduce a new comparison library.
- Default sort must be `{ col: 'created_at', dir: 'desc' }` on load, matching the backend's own `ORDER BY created_at DESC` (`db/sqlite.go:266-268`, `db/mysql.go` equivalent) — the UI default must not contradict the API's natural order.
- This repo has no JS test runner (no `package.json`, no jest/mocha config) and `web/js/app.js` calls `document.addEventListener` at module scope, so it cannot be `require()`'d headlessly. Verification is a manual browser walkthrough (Task 3), not an automated test — do not introduce a JS test framework for this change.

---

### Task 1: Add sort/filter state and logic to `web/js/app.js`

**Files:**
- Modify: `web/js/app.js:66` (Alpine data block, add state next to `downloadFilter`)
- Modify: `web/js/app.js:1203-1207` (`filteredDownloads()`)
- Modify: `web/js/app.js:893-904` region — add new methods near the existing `sortBy`/`sortIndicator` pair, or directly above `filteredDownloads()` in the "--- Downloads ---" section (`web/js/app.js:1201`); either location is fine since these are plain object methods, not order-dependent.

**Interfaces:**
- Produces: `dlSort` (object `{ col, dir }`), `downloadSearch` (string), `dlSortBy(col)`, `dlSortIndicator(col)`, `dlSortValue(dl, col)` — consumed by Task 2's HTML headers.
- Consumes: existing `this.downloads` (array), existing `this.downloadProgress(dl)` (`web/js/app.js:1235-1238`).

- [ ] **Step 1: Add new state fields**

In `web/js/app.js`, find line 66:

```js
        downloads: [],
        downloadFilter: 'all',
```

Replace with:

```js
        downloads: [],
        downloadFilter: 'all',
        downloadSearch: '',
        dlSort: { col: 'created_at', dir: 'desc' },
```

- [ ] **Step 2: Add `dlSortBy`, `dlSortIndicator`, `dlSortValue`**

In `web/js/app.js`, find the "--- Downloads ---" section header at line 1201 and the `filteredDownloads()` method right after it (lines 1203-1207):

```js
        // --- Downloads ---

        filteredDownloads() {
            if (this.downloadFilter === 'all') return this.downloads;
            if (this.downloadFilter === 'failed') return this.downloads.filter(d => d.status === 'failed' || d.status === 'needs_action');
            return this.downloads.filter(d => d.status === this.downloadFilter);
        },
```

Replace with:

```js
        // --- Downloads ---

        dlSortBy(col) {
            if (this.dlSort.col === col) {
                this.dlSort.dir = this.dlSort.dir === 'asc' ? 'desc' : 'asc';
            } else {
                this.dlSort = { col: col, dir: 'asc' };
            }
        },

        dlSortIndicator(col) {
            if (this.dlSort.col !== col) return '';
            return this.dlSort.dir === 'asc' ? ' ▲' : ' ▼';
        },

        dlSortValue(dl, col) {
            if (col === 'filesize') return dl.filesize || dl.total_size || 0;
            if (col === 'progress') return this.downloadProgress(dl);
            if (col === 'speed') {
                if (dl.status === 'downloading') return dl.speed || 0;
                if (dl.status === 'completed') return dl.average_speed || 0;
                return 0;
            }
            return dl[col] == null ? '' : dl[col];
        },

        filteredDownloads() {
            var arr = this.downloads;
            if (this.downloadFilter === 'failed') {
                arr = arr.filter(d => d.status === 'failed' || d.status === 'needs_action');
            } else if (this.downloadFilter !== 'all') {
                arr = arr.filter(d => d.status === this.downloadFilter);
            }
            var q = this.downloadSearch.trim().toLowerCase();
            if (q) {
                arr = arr.filter(d =>
                    (d.filename || '').toLowerCase().includes(q) ||
                    (d.bot_nick || '').toLowerCase().includes(q)
                );
            }
            var col = this.dlSort.col;
            var dir = this.dlSort.dir;
            arr = arr.slice().sort((a, b) => {
                var av = this.dlSortValue(a, col);
                var bv = this.dlSortValue(b, col);
                var cmp;
                if (typeof av === 'number' && typeof bv === 'number') {
                    cmp = av - bv;
                } else {
                    av = String(av).toLowerCase();
                    bv = String(bv).toLowerCase();
                    cmp = av < bv ? -1 : av > bv ? 1 : 0;
                }
                return dir === 'asc' ? cmp : -cmp;
            });
            return arr;
        },
```

- [ ] **Step 3: Verify no other callers break**

Run: `grep -n "filteredDownloads()" web/index.html web/js/app.js`
Expected: same 5 call sites as before (all in `web/index.html`'s downloads-view template, e.g. `filteredDownloads().length`, `dl in filteredDownloads()`) — the method's return type (array) and call signature (no args) are unchanged, so no caller edits are needed.

- [ ] **Step 4: Commit**

```bash
git add web/js/app.js
git commit -m "feat(downloads): add sort and text-filter logic to filteredDownloads"
```

---

### Task 2: Add Date column, sortable headers, and search box to `web/index.html`

**Files:**
- Modify: `web/index.html:818-825` (filter tabs row — add search input)
- Modify: `web/index.html:852-863` (table header row)
- Modify: `web/index.html:880-881,903` (table body — add Date cell)

**Interfaces:**
- Consumes: `dlSortBy(col)`, `dlSortIndicator(col)`, `downloadSearch` from Task 1.

- [ ] **Step 1: Add the search input next to the filter tabs**

Find (`web/index.html:818-825`):

```html
                    <!-- Filter tabs -->
                    <div class="tabs">
                        <div class="tab" :class="{ active: downloadFilter === 'all' }" @click="downloadFilter = 'all'">All</div>
                        <div class="tab" :class="{ active: downloadFilter === 'queued' }" @click="downloadFilter = 'queued'">Queued</div>
                        <div class="tab" :class="{ active: downloadFilter === 'downloading' }" @click="downloadFilter = 'downloading'">Downloading</div>
                        <div class="tab" :class="{ active: downloadFilter === 'completed' }" @click="downloadFilter = 'completed'">Completed</div>
                        <div class="tab" :class="{ active: downloadFilter === 'failed' }" @click="downloadFilter = 'failed'">Failed</div>
                    </div>
```

Replace with (adds a search box using the existing `.search-bar input` styling, right after the tabs row):

```html
                    <!-- Filter tabs -->
                    <div class="tabs">
                        <div class="tab" :class="{ active: downloadFilter === 'all' }" @click="downloadFilter = 'all'">All</div>
                        <div class="tab" :class="{ active: downloadFilter === 'queued' }" @click="downloadFilter = 'queued'">Queued</div>
                        <div class="tab" :class="{ active: downloadFilter === 'downloading' }" @click="downloadFilter = 'downloading'">Downloading</div>
                        <div class="tab" :class="{ active: downloadFilter === 'completed' }" @click="downloadFilter = 'completed'">Completed</div>
                        <div class="tab" :class="{ active: downloadFilter === 'failed' }" @click="downloadFilter = 'failed'">Failed</div>
                    </div>
                    <div class="search-bar" style="padding:6px 12px;border-bottom:1px solid var(--border);">
                        <input type="text" x-model="downloadSearch" placeholder="Filter by filename or bot...">
                    </div>
```

- [ ] **Step 2: Make table headers sortable and add the Date column**

Find (`web/index.html:852-863`):

```html
                                <thead>
                                    <tr>
                                        <th style="width:28px;"></th>
                                        <th>Status</th>
                                        <th>Filename</th>
                                        <th>Bot</th>
                                        <th>Size</th>
                                        <th>Progress</th>
                                        <th>Speed / Info</th>
                                        <th>Time</th>
                                        <th></th>
                                    </tr>
                                </thead>
```

Replace with:

```html
                                <thead>
                                    <tr>
                                        <th style="width:28px;"></th>
                                        <th>Status</th>
                                        <th @click="dlSortBy('filename')" x-text="'Filename' + dlSortIndicator('filename')"></th>
                                        <th @click="dlSortBy('bot_nick')" x-text="'Bot' + dlSortIndicator('bot_nick')"></th>
                                        <th @click="dlSortBy('filesize')" x-text="'Size' + dlSortIndicator('filesize')"></th>
                                        <th @click="dlSortBy('progress')" x-text="'Progress' + dlSortIndicator('progress')"></th>
                                        <th @click="dlSortBy('speed')" x-text="'Speed / Info' + dlSortIndicator('speed')"></th>
                                        <th @click="dlSortBy('created_at')" x-text="'Date' + dlSortIndicator('created_at')"></th>
                                        <th>Time</th>
                                        <th></th>
                                    </tr>
                                </thead>
```

(`th` is already styled with `cursor: pointer` for sortable headers, per `web/css/style.css:161` used by the search-results table — no new CSS needed. Status has no click handler, matching the plan's decision that Status sorting is redundant with the filter tabs.)

- [ ] **Step 3: Add the Date cell to each table row**

Find (`web/index.html:880-903`, the `<td>` block starting right after the Status `<td>` and ending at the Time `<td>`):

```html
                                            <td x-text="dl.filename || '-'"></td>
                                            <td>
                                                <span class="tooltip">
                                                    <span x-text="dl.bot_nick || '-'"></span>
                                                    <span class="tooltip-text" x-text="dl.filesize || '-'"></span>
                                                </span>
                                            </td>
                                            <td x-text="dl.filesize ? formatSize(dl.filesize) : '-'"></td>
                                            <td>
                                                <template x-if="dl.status === 'downloading'">
                                                    <div class="progress">
                                                        <div
                                                            class="progress-fill"
                                                            :style="'width:' + downloadProgress(dl) + '%'"
                                                        ></div>
                                                    </div>
                                                </template>
                                                <template x-if="dl.status !== 'downloading'">
                                                    <span x-text="dl.status === 'completed' ? '100%' : '-'"></span>
                                                </template>
                                            </td>
                                            <td x-text="dlSpeedInfo(dl)"></td>
                                            <td :title="dlTimeTooltip(dl)" x-text="dlTimeInfo(dl)"></td>
```

Replace with (only change: one new `<td>` inserted between Speed/Info and Time):

```html
                                            <td x-text="dl.filename || '-'"></td>
                                            <td>
                                                <span class="tooltip">
                                                    <span x-text="dl.bot_nick || '-'"></span>
                                                    <span class="tooltip-text" x-text="dl.filesize || '-'"></span>
                                                </span>
                                            </td>
                                            <td x-text="dl.filesize ? formatSize(dl.filesize) : '-'"></td>
                                            <td>
                                                <template x-if="dl.status === 'downloading'">
                                                    <div class="progress">
                                                        <div
                                                            class="progress-fill"
                                                            :style="'width:' + downloadProgress(dl) + '%'"
                                                        ></div>
                                                    </div>
                                                </template>
                                                <template x-if="dl.status !== 'downloading'">
                                                    <span x-text="dl.status === 'completed' ? '100%' : '-'"></span>
                                                </template>
                                            </td>
                                            <td x-text="dlSpeedInfo(dl)"></td>
                                            <td x-text="fmtDate(dl.created_at)"></td>
                                            <td :title="dlTimeTooltip(dl)" x-text="dlTimeInfo(dl)"></td>
```

- [ ] **Step 4: Commit**

```bash
git add web/index.html
git commit -m "feat(downloads): add Date column and sortable headers to downloads table"
```

---

### Task 3: Manual verification

No JS test framework exists in this repo (see Global Constraints), so verification is a manual browser walkthrough. The server is started manually by the user (per project instructions, port 8085) — do not start it yourself; ask the user to start it if it isn't already running, then drive the browser check.

- [ ] **Step 1: Load the Downloads view**

Navigate to the app, click "Downloads" in the sidebar. Confirm:
- A "Date" column appears between "Speed / Info" and "Time", showing formatted dates (not blank/`NaN`) for every row that has `created_at`.
- Rows are ordered newest-first by default (top row's Date is the most recent).

- [ ] **Step 2: Check sorting**

Click the "Filename" header. Confirm rows reorder alphabetically ascending and an "▲" appears next to "Filename". Click it again — confirm descending order and "▼". Click "Date" — confirm it re-sorts by date and the arrow moves to the Date column (only one column shows an indicator at a time).

- [ ] **Step 3: Check the text filter**

Type a partial filename (or bot nick) into the new filter box above the table. Confirm only matching rows remain, and combining it with a status tab (e.g. click "Completed" then type text) narrows correctly (both filters apply together — AND, not OR).

- [ ] **Step 4: Confirm no regressions**

Confirm existing controls still work: Select All checkbox, Clear Completed/Clear Failed/Clear Selected buttons, Cancel/Retry/Move to front buttons, Extract checkbox on archives. Confirm the earlier button-contrast fix (`.btn` base background in `web/css/style.css`) still reads correctly on the "Clear Completed"/"Clear Failed" buttons in this view.

- [ ] **Step 5: Commit any fixes found during verification**

If Steps 1-4 surface a bug, fix it, re-run the relevant step, then:

```bash
git add -u
git commit -m "fix(downloads): <describe the specific fix>"
```

If no issues are found, no commit is needed for this task.
