document.addEventListener('alpine:init', () => {
    Alpine.data('appStore', () => ({
        servers: [],
        ircStatus: {},
        activeView: 'channel',
        activeServer: null,
        activeChannel: null,
        expandedServers: {},
        searchQuery: '',
        searchResults: [],
        searchRunning: false,
        savedSearches: [],
        searchSort: { col: 'pack_number', dir: 'asc' },
        searchToken: 0,
        downloads: [],
        downloadFilter: 'all',
        ircMessages: {},
        ircInput: '',
        showIrcConsole: false,
        ws: null,
        wsRetryTimer: null,

        // Settings state
        settingsTab: 'servers',
        settingsServers: [],
        settingsChannels: [],
        settingsServerId: null,
        routingRules: [],
        hooks: [],
        serverForm: { id: null, name: '', host: '', port: 6667, nickname: '', ssl: false, auto_connect: false, enabled: true },
        channelForm: { id: null, server_id: null, name: '', search_command: '', download_channel: '', auto_join: false },
        routingForm: { id: null, pattern: '', destination_dir: '', priority: 0 },
        hookForm: { id: null, name: '', scope: 'global', hook_type: 'script', config: '', enabled: true },
        showServerForm: false,
        showChannelForm: false,
        showRoutingForm: false,
        showHookForm: false,
        routingNewDir: '',
        routingNewExt: '',
        showAddDestForm: false,

        // Mode
        appMode: localStorage.getItem('xirc_mode') || 'simple',

        // Channel configs map: channelKey -> download_channel
        _channelConfigs: {},

        // Active server (for server view)
        activeServerObj: null,

        // Server view
        serverMessages: {},  // keyed by server_id, value: [{timestamp,nick,text,msg_type}]
        serverMsgLoading: {},

        // Channel view
        _channelLayout: {},   // NOTE: underscore prefix to avoid collision with channelLayout() method
        activeChannelTab: {},
        userLists: {},
        userListLoading: {},
        selectedUser: null,

        // Dir picker
        dirPickerOpen: false,
        dirPickerPath: '/',
        dirPickerEntries: [],
        dirPickerCallback: null,

        // File manager
        fileManagerDir: null,
        fileManagerFiles: [],
        fileManagerLoading: false,

        // Errors
        errors: [],
        unreadErrors: 0,

        // Stats
        statsSummary: null,
        statsHistory: [],
        statsHistoryOffset: 0,
        statsLoading: false,

        // Sidebar mobile
        sidebarOpen: false,

        // Stats-only setting
        statsOnlyDefault: false,

        // Setup wizard
        setupRequired: false,
        setupMappings: [],  // [{old_dir, new_dir, suggestion}]
        setupBanner: false,

        // Global search filter (advanced mode)
        globalSearchServerId: '',

        // Disk stats
        diskStats: [],

        // --- Computed helpers ---

        groupedRoutingRules() {
            var groups = {};
            for (var i = 0; i < this.routingRules.length; i++) {
                var r = this.routingRules[i];
                var dir = r.destination_dir;
                if (!groups[dir]) groups[dir] = [];
                var ext = r.pattern.replace(/^\*\./, '');
                groups[dir].push({ id: r.id, ext: ext, rule: r });
            }
            return groups;
        },

        predefinedExtensions() {
            return ['mkv', 'mp4', 'avi', 'mp3', 'flac', 'epub', 'pdf', 'zip', 'cbz'];
        },

        serverStatus(serverId) {
            const s = this.ircStatus[serverId];
            return s ? s.status : 'disconnected';
        },

        channelKey(serverId, channel) {
            return serverId + ':' + channel;
        },

        currentChannelMessages() {
            if (!this.activeServer || !this.activeChannel) return [];
            return this.ircMessages[this.channelKey(this.activeServer, this.activeChannel)] || [];
        },

        // --- Navigation ---

        setView(view) {
            this.activeView = view;
            if (view === 'settings') {
                this.showServerForm = false;
                this.showChannelForm = false;
                this.showRoutingForm = false;
                this.showHookForm = false;
                this.loadSettingsData();
            } else if (view === 'files') {
                api.getRoutingRules().then(r => { this.routingRules = Array.isArray(r) ? r : []; }).catch(console.error);
            }
        },

        selectChannel(serverId, channelName) {
            this.activeServer = serverId;
            this.activeChannel = channelName;
            this.activeView = 'channel';
        },

        toggleServer(serverId) {
            this.expandedServers[serverId] = !this.expandedServers[serverId];
        },

        isExpanded(serverId) {
            return !!this.expandedServers[serverId];
        },

        setMode(mode) {
            this.appMode = mode;
            localStorage.setItem('xirc_mode', mode);
        },

        // --- Server view ---

        async selectServer(server) {
            this.activeServerObj = server;
            this.activeServer = server.id;
            this.activeChannel = null;
            this.activeView = 'server';
            await this.loadServerMessages(server.id);
        },

        async loadServerMessages(serverId) {
            this.serverMsgLoading[serverId] = true;
            try {
                const msgs = await api.getIRCMessages(serverId, '', null, 200);
                this.serverMessages[serverId] = Array.isArray(msgs) ? msgs : [];
            } catch(e) {
                console.error('loadServerMessages', e);
            } finally {
                this.serverMsgLoading[serverId] = false;
            }
        },

        async loadMoreServerMessages(serverId) {
            const existing = this.serverMessages[serverId] || [];
            if (existing.length === 0) return;
            const oldest = existing[0].timestamp;
            try {
                const msgs = await api.getIRCMessages(serverId, '', oldest, 200);
                if (msgs && msgs.length > 0) {
                    this.serverMessages[serverId] = msgs.concat(existing);
                }
            } catch(e) {
                console.error('loadMoreServerMessages', e);
            }
        },

        // --- Channel layout ---

        channelLayout(key) {
            return this._channelLayout[key] || 'single';
        },
        setChannelLayout(key, layout) {
            this._channelLayout = Object.assign({}, this._channelLayout, {[key]: layout});
            localStorage.setItem('xirc_layout_' + key, layout);
        },
        activeTab(key) {
            return this.activeChannelTab[key] || 'search';
        },
        setActiveTab(key, tab) {
            this.activeChannelTab = Object.assign({}, this.activeChannelTab, {[key]: tab});
        },

        // --- User list ---

        async loadUserList(serverId, channel) {
            const key = this.channelKey(serverId, channel);
            this.userListLoading[key] = true;
            try {
                const res = await api.getIRCNames(serverId, channel);
                this.userLists[key] = res && res.nicks ? res.nicks : [];
            } catch(e) {
                console.error('loadUserList', e);
            } finally {
                this.userListLoading[key] = false;
            }
        },

        selectUser(nick) {
            this.selectedUser = this.selectedUser === nick ? null : nick;
        },

        isUserSelected(nick) {
            return this.selectedUser === nick;
        },

        userHighlightClass(nick) {
            return this.selectedUser && nick === this.selectedUser ? 'bg-yellow-100 font-bold' : '';
        },

        // --- Channel helpers ---

        currentChannelHasDownloadChannel() {
            if (!this.activeServer || !this.activeChannel) return false;
            const dl = this.downloadChannelForCurrent();
            return dl && dl !== this.activeChannel;
        },

        downloadChannelForCurrent() {
            return this._channelConfigs && this._channelConfigs[this.channelKey(this.activeServer, this.activeChannel)];
        },

        sendIrcToChannel(serverId, channel) {
            const text = this.ircInput.trim();
            if (!text || !serverId || !channel) return;
            this.ircInput = '';
            api.sendMessage(serverId, channel, text).catch(console.error);
        },

        // --- IRC actions ---

        async sendIrc() {
            const text = this.ircInput.trim();
            if (!text || !this.activeServer) return;
            this.ircInput = '';
            if (text.startsWith('/')) {
                await api.sendRaw(this.activeServer, text.slice(1));
            } else {
                if (!this.activeChannel) return;
                await api.sendMessage(this.activeServer, this.activeChannel, text);
            }
        },

        // --- Search ---

        async runSearch() {
            if (!this.activeServer || !this.activeChannel || !this.searchQuery.trim()) return;
            const token = ++this.searchToken;
            this.searchRunning = true;
            this.searchResults = [];
            try {
                await api.startSearch(this.activeServer, this.activeChannel, this.searchQuery);
                // Poll up to 5 times with 1s delay; stop early when results arrive.
                var results = [];
                for (var attempt = 0; attempt < 5; attempt++) {
                    await new Promise(function(resolve) { setTimeout(resolve, 1000); });
                    var res = await api.getSearchResults(this.searchQuery, this.activeServer, this.activeChannel);
                    results = res || [];
                    if (results.length > 0) break;
                }
                if (token !== this.searchToken) return;
                this.searchResults = results;
            } catch (e) {
                console.error('search error', e);
            } finally {
                if (token === this.searchToken) this.searchRunning = false;
            }
        },

        async loadSavedSearches() {
            try {
                var res = await api.getSavedSearches();
                this.savedSearches = Array.isArray(res) ? res : [];
            } catch (e) {
                console.error('loadSavedSearches error', e);
            }
        },

        async bookmarkSearch() {
            if (!this.searchQuery.trim()) return;
            try {
                await api.createSavedSearch({
                    query: this.searchQuery,
                    server_id: this.activeServer,
                    channel: this.activeChannel,
                });
                await this.loadSavedSearches();
            } catch (e) {
                console.error('bookmarkSearch error', e);
            }
        },

        applySavedSearch(query) {
            if (!query) return;
            this.searchQuery = query;
            this.runGlobalSearch();
        },

        // Global search across all channels (or filtered by globalSearchServerId in advanced mode).
        async runGlobalSearch() {
            if (!this.searchQuery.trim()) return;
            const token = ++this.searchToken;
            this.searchRunning = true;
            this.searchResults = [];
            try {
                // Fire search on all matching channels
                const targets = [];
                for (var i = 0; i < this.servers.length; i++) {
                    var srv = this.servers[i];
                    if (this.globalSearchServerId && srv.id !== parseInt(this.globalSearchServerId)) continue;
                    var channels = srv.channels || [];
                    for (var j = 0; j < channels.length; j++) {
                        targets.push({ server_id: srv.id, channel: channels[j] });
                    }
                }
                // Kick off searches (ignore errors — bot may not be present on all channels)
                for (var k = 0; k < targets.length; k++) {
                    api.startSearch(targets[k].server_id, targets[k].channel, this.searchQuery).catch(function(){});
                }
                // Poll for aggregated results
                for (var attempt = 0; attempt < 8; attempt++) {
                    await new Promise(function(resolve) { setTimeout(resolve, 1000); });
                    if (token !== this.searchToken) return;
                    var res = await api.getAllSearchResults(this.searchQuery);
                    this.searchResults = Array.isArray(res) ? res : [];
                    if (this.searchResults.length > 0) break;
                }
            } catch (e) {
                console.error('global search error', e);
            } finally {
                if (token === this.searchToken) this.searchRunning = false;
            }
        },

        getServerName(serverId) {
            for (var i = 0; i < this.servers.length; i++) {
                if (this.servers[i].id === serverId) return this.servers[i].name;
            }
            return 'Server ' + serverId;
        },

        async loadDiskStats() {
            try {
                var res = await api.getStorageStats();
                this.diskStats = Array.isArray(res) ? res : [];
            } catch (e) {
                console.error('disk stats error', e);
            }
        },

        sortBy(col) {
            if (this.searchSort.col === col) {
                this.searchSort.dir = this.searchSort.dir === 'asc' ? 'desc' : 'asc';
            } else {
                this.searchSort = { col: col, dir: 'asc' };
            }
        },

        sortIndicator(col) {
            if (this.searchSort.col !== col) return '';
            return this.searchSort.dir === 'asc' ? ' ▲' : ' ▼';
        },

        sortedResults() {
            var col = this.searchSort.col;
            var dir = this.searchSort.dir;
            var arr = this.searchResults.slice();
            if (this.selectedUser) {
                arr = arr.filter(function(r) {
                    return r.bot_nick === this.selectedUser;
                }.bind(this));
            }
            arr.sort(function(a, b) {
                var av = a[col];
                var bv = b[col];
                if (av == null) av = '';
                if (bv == null) bv = '';
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

        downloadPack(row) {
            api.requestDownload({
                server_id: this.activeServer,
                channel: this.activeChannel,
                bot_nick: row.bot_nick,
                pack_number: row.pack_number,
                stats_only: this.statsOnlyDefault,
            }).catch(function(e) { console.error('download request error', e); });
        },

        teachParser(rawLine) {
            alert(rawLine);
        },

        // --- Downloads ---

        filteredDownloads() {
            if (this.downloadFilter === 'all') return this.downloads;
            if (this.downloadFilter === 'failed') return this.downloads.filter(d => d.status === 'failed' || d.status === 'needs_action');
            return this.downloads.filter(d => d.status === this.downloadFilter);
        },

        async loadDownloads() {
            try {
                const res = await api.getDownloads();
                this.downloads = Array.isArray(res) ? res : [];
            } catch (e) {
                console.error('loadDownloads error', e);
            }
        },

        cancelDownload(id) {
            api.cancelDownload(id).then(() => this.loadDownloads()).catch(console.error);
        },

        retryDownload(id) {
            api.retryDownload(id).then(() => this.loadDownloads()).catch(console.error);
        },

        moveToFront(id) {
            api.moveDownload(id).then(() => this.loadDownloads()).catch(console.error);
        },

        downloadProgress(dl) {
            if (!dl.total_size) return 0;
            return Math.round((dl.bytes_received / dl.total_size) * 100);
        },

        // --- WebSocket ---

        wsConnect() {
            if (this.ws) {
                this.ws.close();
                this.ws = null;
            }
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            const url = proto + '//' + location.host + '/ws';
            let sock;
            try {
                sock = new WebSocket(url);
            } catch (e) {
                console.error('WebSocket create error', e);
                this._scheduleWsRetry();
                return;
            }
            this.ws = sock;

            sock.onmessage = (ev) => {
                let data;
                try { data = JSON.parse(ev.data); } catch (e) { return; }
                this._handleWsEvent(data);
            };

            sock.onclose = () => {
                this.ws = null;
                this._scheduleWsRetry();
            };

            sock.onerror = (e) => {
                console.error('WebSocket error', e);
            };
        },

        _scheduleWsRetry() {
            if (this.wsRetryTimer) return;
            this.wsRetryTimer = setTimeout(() => {
                this.wsRetryTimer = null;
                this.wsConnect();
            }, 5000);
        },

        _handleWsEvent(data) {
            const type = data.type;

            if (type === 'irc_message') {
                if (data.channel && data.channel !== '') {
                    const key = this.channelKey(data.server_id, data.channel);
                    if (!this.ircMessages[key]) {
                        this.ircMessages[key] = [];
                    }
                    this.ircMessages[key].push({
                        nick: data.nick,
                        message: data.message,
                        timestamp: data.timestamp,
                    });
                    // Keep buffer bounded
                    if (this.ircMessages[key].length > 500) {
                        this.ircMessages[key].splice(0, this.ircMessages[key].length - 500);
                    }
                    // Auto-scroll if this message is for the active channel
                    if (data.server_id === this.activeServer && data.channel === this.activeChannel) {
                        this.$nextTick(() => {
                            const el = this.$refs && this.$refs.ircLog;
                            if (el) el.scrollTop = el.scrollHeight;
                        });
                    }
                } else {
                    // Server-level message (no channel): buffer in serverMessages only
                    if (!this.serverMessages[data.server_id]) {
                        this.serverMessages[data.server_id] = [];
                    }
                    this.serverMessages[data.server_id].push({
                        nick: data.nick,
                        message: data.message || (data.data && data.data.message) || '',
                        timestamp: data.timestamp,
                    });
                    if (this.serverMessages[data.server_id].length > 500) {
                        this.serverMessages[data.server_id].splice(0, this.serverMessages[data.server_id].length - 500);
                    }
                }
            } else if (type === 'download_progress') {
                const p = data.data;
                const dl = this.downloads.find(d => d.id === p.download_id);
                if (dl) {
                    dl.bytes_received = p.bytes_received;
                    dl.total_size = p.total_size;
                    dl.speed = p.speed;
                }
            } else if (type === 'download_complete') {
                this.loadDownloads();
            } else if (type === 'download_failed') {
                this.loadDownloads();
            } else if (type === 'connection_status') {
                const d = data.data;
                const status = typeof d === 'string' ? d : d.status;
                this.ircStatus[data.server_id] = Object.assign({}, this.ircStatus[data.server_id] || {}, {
                    status: status,
                    connected_at: d.connected_at || (this.ircStatus[data.server_id] || {}).connected_at,
                    reconnect_count: d.reconnect_count !== undefined ? d.reconnect_count : (this.ircStatus[data.server_id] || {}).reconnect_count,
                    lag_ms: d.lag_ms !== undefined ? d.lag_ms : (this.ircStatus[data.server_id] || {}).lag_ms,
                });
            } else if (type === 'error_event') {
                this.errors.unshift(data.data || data);
                if (this.errors.length > 200) this.errors.pop();
                this.unreadErrors++;
            }
        },

        // --- Dir picker ---

        async openDirPicker(currentPath, callback) {
            this.dirPickerCallback = callback;
            this.dirPickerOpen = true;
            await this.browseDir(currentPath || '/');
        },

        async browseDir(path) {
            try {
                const res = await api.browseDir(path);
                this.dirPickerPath = res.path;
                this.dirPickerEntries = res.entries || [];
            } catch(e) {
                console.error('browseDir', e);
            }
        },

        async browseDirUp() {
            const parent = this.dirPickerPath.split('/').slice(0, -1).join('/') || '/';
            await this.browseDir(parent);
        },

        selectDir() {
            if (this.dirPickerCallback) {
                this.dirPickerCallback(this.dirPickerPath);
            }
            this.dirPickerOpen = false;
            this.dirPickerCallback = null;
        },

        cancelDirPicker() {
            this.dirPickerOpen = false;
            this.dirPickerCallback = null;
        },

        // --- File manager ---

        async loadFileManager(dir) {
            this.fileManagerDir = dir;
            this.fileManagerLoading = true;
            try {
                const res = await api.listFiles(dir);
                this.fileManagerFiles = res && res.files ? res.files : [];
            } catch(e) {
                console.error('loadFileManager', e);
            } finally {
                this.fileManagerLoading = false;
            }
        },

        // --- Stats ---

        async loadStats() {
            this.statsLoading = true;
            try {
                const [summary, history] = await Promise.all([
                    api.getDownloadStats(),
                    api.getDownloadHistory(0, 50),
                ]);
                this.statsSummary = summary;
                this.statsHistory = Array.isArray(history) ? history : [];
                this.statsHistoryOffset = 50;
            } catch(e) {
                console.error('loadStats', e);
            } finally {
                this.statsLoading = false;
            }
        },

        async loadMoreHistory() {
            try {
                const more = await api.getDownloadHistory(this.statsHistoryOffset, 50);
                if (more && more.length > 0) {
                    this.statsHistory = this.statsHistory.concat(more);
                    this.statsHistoryOffset += more.length;
                }
            } catch(e) {
                console.error('loadMoreHistory', e);
            }
        },

        // --- Errors ---

        async loadErrors() {
            try {
                const errs = await api.getErrors(100);
                this.errors = Array.isArray(errs) ? errs : [];
            } catch(e) {
                console.error('loadErrors', e);
            }
        },

        clearErrors() {
            this.errors = [];
            this.unreadErrors = 0;
        },

        // --- Settings ---

        loadSettingsData() {
            return Promise.all([
                api.getServers().then(r => { this.settingsServers = Array.isArray(r) ? r : []; }),
                api.getRoutingRules().then(r => { this.routingRules = Array.isArray(r) ? r : []; }),
                api.getHooks().then(r => { this.hooks = Array.isArray(r) ? r : []; }),
            ]);
        },

        loadSettingsChannels(serverId) {
            this.settingsServerId = serverId;
            return api.getChannels(serverId).then(r => { this.settingsChannels = Array.isArray(r) ? r : []; });
        },

        openServerForm(server) {
            this.serverForm = server
                ? Object.assign({}, server)
                : { id: null, name: '', host: '', port: 6667, nickname: '', ssl: false, auto_connect: false, enabled: true };
            this.showServerForm = true;
        },

        saveServer() {
            const p = this.serverForm.id
                ? api.updateServer(this.serverForm.id, this.serverForm)
                : api.createServer(this.serverForm);
            return p.then(() => {
                this.showServerForm = false;
                return this.loadSettingsData();
            }).catch(console.error);
        },

        deleteServer(id) {
            if (!confirm('Delete server?')) return;
            return api.deleteServer(id).then(() => this.loadSettingsData()).catch(console.error);
        },

        openChannelForm(channel) {
            this.channelForm = channel
                ? Object.assign({}, channel)
                : { id: null, server_id: this.settingsServerId, name: '', search_command: '', download_channel: '', auto_join: false };
            this.showChannelForm = true;
        },

        saveChannel() {
            const p = this.channelForm.id
                ? api.updateChannel(this.channelForm.id, this.channelForm)
                : api.createChannel(this.channelForm);
            return p.then(() => {
                this.showChannelForm = false;
                return this.loadSettingsChannels(this.settingsServerId);
            }).catch(console.error);
        },

        deleteChannel(id) {
            if (!confirm('Delete channel?')) return;
            return api.deleteChannel(id).then(() => this.loadSettingsChannels(this.settingsServerId)).catch(console.error);
        },

        openRoutingForm(rule) {
            this.routingForm = rule
                ? Object.assign({}, rule)
                : { id: null, pattern: '', destination_dir: '', priority: 0 };
            this.showRoutingForm = true;
        },

        saveRoutingRule() {
            const p = this.routingForm.id
                ? api.updateRoutingRule(this.routingForm.id, this.routingForm)
                : api.createRoutingRule(this.routingForm);
            return p.then(() => {
                this.showRoutingForm = false;
                return this.loadSettingsData();
            }).catch(console.error);
        },

        deleteRoutingRule(id) {
            if (!confirm('Delete routing rule?')) return;
            return api.deleteRoutingRule(id).then(() => this.loadSettingsData()).catch(console.error);
        },

        openHookForm(hook) {
            this.hookForm = hook
                ? Object.assign({}, hook)
                : { id: null, name: '', scope: 'global', hook_type: 'script', config: '', enabled: true };
            this.showHookForm = true;
        },

        saveHook() {
            const p = this.hookForm.id
                ? api.updateHook(this.hookForm.id, this.hookForm)
                : api.createHook(this.hookForm);
            return p.then(() => {
                this.showHookForm = false;
                return this.loadSettingsData();
            }).catch(console.error);
        },

        deleteHook(id) {
            if (!confirm('Delete hook?')) return;
            return api.deleteHook(id).then(() => this.loadSettingsData()).catch(console.error);
        },

        async addRoutingRuleForDir(dir, ext) {
            if (!dir || !ext) return;
            const pattern = '*.' + ext.replace(/^\./, '');
            await api.createRoutingRule({ pattern, destination_dir: dir, priority: 0 });
            await this.loadSettingsData();
        },

        async renameRoutingDir(oldDir, newDir) {
            if (!newDir || newDir === oldDir) return;
            const groups = this.groupedRoutingRules();
            const rules = groups[oldDir] || [];
            for (const item of rules) {
                await api.updateRoutingRule(item.id, Object.assign({}, item.rule, { destination_dir: newDir }));
            }
            await this.loadSettingsData();
        },

        // --- Setup wizard ---

        async checkSetup() {
            try {
                const status = await api.getSetupStatus();
                if (!status.required) return;
                this.setupRequired = true;
                try {
                    const defaults = await api.getSetupDefaults();
                    this.setupMappings = (status.bad_dirs || []).map(dir => ({
                        old_dir: dir,
                        new_dir: dir.toLowerCase().includes('download') ? defaults.downloads_dir : defaults.videos_dir,
                        suggestion: dir.toLowerCase().includes('download') ? defaults.downloads_dir : defaults.videos_dir,
                    }));
                } catch (e) {
                    console.error('setup defaults error', e);
                    this.setupRequired = false;
                }
            } catch (e) {
                console.error('setup check error', e);
            }
        },

        async applySetup() {
            const mappings = this.setupMappings.map(m => ({ old_dir: m.old_dir, new_dir: m.new_dir }));
            try {
                await api.completeSetup(mappings);
                this.setupRequired = false;
                this.setupBanner = true;
            } catch (e) {
                console.error('setup complete error', e);
            }
        },

        cancelSetup() {
            // Apply defaults without prompting
            this.setupMappings = this.setupMappings.map(m => ({ ...m, new_dir: m.suggestion }));
            this.applySetup();
        },

        // --- Init ---

        async init() {
            await this.checkSetup();

            // Load disk stats and refresh every 30 seconds
            await this.loadDiskStats();
            var self = this;
            setInterval(function() { self.loadDiskStats(); }, 30000);

            // Restore layout prefs from localStorage
            this._channelLayout = {};
            for (let i = 0; i < localStorage.length; i++) {
                const k = localStorage.key(i);
                if (k && k.startsWith('xirc_layout_')) {
                    this._channelLayout[k.slice('xirc_layout_'.length)] = localStorage.getItem(k);
                }
            }
            this.statsOnlyDefault = localStorage.getItem('xirc_stats_only') === 'true';

            // Load servers
            try {
                const res = await api.getServers();
                this.servers = Array.isArray(res) ? res : [];
                if (this.servers.length > 0) {
                    this.expandedServers[this.servers[0].id] = true;
                }
            } catch (e) {
                console.error('loadServers error', e);
            }

            // Load IRC status and merge channels into server objects
            try {
                const res = await api.getIRCStatus();
                const statuses = Array.isArray(res) ? res : [];
                const statusMap = {};
                for (const s of statuses) {
                    statusMap[s.server_id] = {
                        status: s.status,
                        channels: s.channels,
                        name: s.server_name,
                        connected_at: s.connected_at,
                        reconnect_count: s.reconnect_count,
                        lag_ms: s.lag_ms,
                    };
                }
                this.ircStatus = statusMap;
                // Merge channels from IRC status into servers array
                this.servers = this.servers.map(srv => {
                    const st = statusMap[srv.id];
                    return Object.assign({}, srv, {
                        channels: (st && st.channels) ? st.channels : (srv.channels || []),
                    });
                });
            } catch (e) {
                console.error('loadIRCStatus error', e);
            }

            // Load channel configs (for download_channel mapping)
            for (const srv of this.servers) {
                const channels = await api.getChannels(srv.id).catch(() => []);
                for (const ch of (channels || [])) {
                    const key = this.channelKey(srv.id, ch.name);
                    this._channelConfigs[key] = ch.download_channel || ch.name;
                }
            }

            // Load downloads
            await this.loadDownloads();

            // Load saved searches
            await this.loadSavedSearches();

            // Load errors
            await this.loadErrors();

            // Start WebSocket
            this.wsConnect();
        },
    }));
});
