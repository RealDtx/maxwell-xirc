function escHtml(s) {
    return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function getCharOffset(container, node, offset) {
    // Walk text nodes in container up to (node, offset), summing lengths
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
    let count = 0;
    let current;
    while ((current = walker.nextNode())) {
        if (current === node) {
            count += offset;
            break;
        }
        count += current.textContent.length;
    }
    return count;
}

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
        patternTrainer: {
            open: false,
            rawLine: '',
            annotations: [],
            fieldPickerVisible: false,
            fieldPickerX: 0,
            fieldPickerY: 0,
            pendingStart: null,
            pendingEnd: null,
            patternName: '',
            previewRegex: '',
            previewFieldMapping: '',
            saving: false,
            savedCount: null,
            error: '',
        },
        _trainerFields: [
            { field: 'filename',        label: 'Filename',      color: '#4a9eff' },
            { field: 'pack_number',     label: 'Pack #',        color: '#f59e0b' },
            { field: 'filesize',        label: 'File Size',     color: '#10b981' },
            { field: 'bot_nick',        label: 'Bot Nick',      color: '#8b5cf6' },
            { field: 'downloads_count', label: 'Downloads',     color: '#ef4444' },
            { field: 'skip_number',     label: 'Skip (number)', color: '#6b7280' },
            { field: 'skip',            label: 'Skip (text)',   color: '#6b7280' },
        ],
        searchRunning: false,
        savedSearches: [],
        searchSort: { col: 'pack_number', dir: 'asc' },
        searchToken: 0,
        _searchSince: null,
        _searchDisplayLimit: 200,
        searchBotWarning: null,
        downloads: [],
        downloadFilter: 'all',
        ircMessages: {},
        _msgVersion: 0,  // bumped whenever ircMessages or serverMessages change; forces x-for re-eval
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
        channelForm: { id: null, server_id: null, name: '', search_command: '', download_channel: '', search_bot: '', search_timeout: 10, auto_join: false, enabled: true },
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
        dateFormat: localStorage.getItem('xirc_date_format') || 'DD-MM-YYYY HH:MM:SS',

        // Channel configs map: channelKey -> download_channel
        _channelConfigs: {},
        // Realm configs map: channelKey -> realm object {id, name, display_name, download_channel, search_command}
        _realmConfigs: {},
        // Settings - Realms (replaces settingsChannels)
        realms: [],
        settingsRealms: [],
        realmForm: { id: null, server_id: null, name: '', display_name: '', search_command: '', download_channel: '', search_bot: '', search_timeout: 10, auto_join: false, enabled: true },
        showRealmForm: false,

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

        // Download button state tracking: "serverID:packNumber" → 'queuing'|'queued'
        _downloadingKeys: {},
        _dlSelected: {},
        _dlSelectAll: false,
        _dlConfirm: null,
        _dlConfirmTimer: null,

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
            void this._msgVersion; // track _msgVersion so x-for re-evaluates when messages arrive
            if (!this.activeServer || !this.activeChannel) return [];
            return this.ircMessages[this.channelKey(this.activeServer, this.activeChannel)] || [];
        },

        currentServerMessages(serverId) {
            void this._msgVersion; // track _msgVersion so x-for re-evaluates when messages arrive
            if (!serverId) return [];
            return this.serverMessages[serverId] || [];
        },

        currentDownloadChannelMessages() {
            void this._msgVersion;
            if (!this.activeServer || !this.activeChannel) return [];
            const dlChan = this.downloadChannelForCurrent();
            if (!dlChan) return [];
            return this.ircMessages[this.channelKey(this.activeServer, dlChan)] || [];
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
            this.loadChannelMessages(serverId, channelName);
            // Also pre-load download channel if one is configured
            const dlKey = this.channelKey(serverId, channelName);
            const dlChan = this._channelConfigs && this._channelConfigs[dlKey];
            if (dlChan && dlChan !== channelName) {
                this.loadChannelMessages(serverId, dlChan);
            }
        },

        async loadChannelMessages(serverId, channelName) {
            const key = this.channelKey(serverId, channelName);
            try {
                const msgs = await api.getIRCMessages(serverId, channelName, null, 200);
                if (Array.isArray(msgs)) {
                    this.ircMessages = Object.assign({}, this.ircMessages, {
                        [key]: msgs
                            .map(m => ({ nick: m.nick, message: m.text || m.message || '', timestamp: m.timestamp, msg_type: m.msg_type || 'privmsg' }))
                            .filter(m => !(m.msg_type === 'raw' && formatRawLine(m.message) === ''))
                    });
                    this._msgVersion++;
                }
            } catch(e) {
                console.error('loadChannelMessages', e);
            }
        },

        async loadMoreChannelMessages(serverId, channelName) {
            const key = this.channelKey(serverId, channelName);
            const existing = this.ircMessages[key] || [];
            if (existing.length === 0) return;
            const oldest = existing[0].timestamp;
            try {
                const msgs = await api.getIRCMessages(serverId, channelName, oldest, 200);
                if (msgs && msgs.length > 0) {
                    const normalized = msgs
                        .map(m => ({ nick: m.nick, message: m.text || m.message || '', timestamp: m.timestamp, msg_type: m.msg_type || 'privmsg' }))
                        .filter(m => !(m.msg_type === 'raw' && formatRawLine(m.message) === ''));
                    this.ircMessages = Object.assign({}, this.ircMessages, {
                        [key]: normalized.concat(existing)
                    });
                    this._msgVersion++;
                }
            } catch(e) {
                console.error('loadMoreChannelMessages', e);
            }
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
                this.serverMessages = Object.assign({}, this.serverMessages, {
                    [serverId]: Array.isArray(msgs) ? msgs : []
                });
                this._msgVersion++;
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
                    this.serverMessages = Object.assign({}, this.serverMessages, {
                        [serverId]: msgs.concat(existing)
                    });
                    this._msgVersion++;
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
            return this.activeChannelTab[key] || 'chat';
        },
        setActiveTab(key, tab) {
            this.activeChannelTab = Object.assign({}, this.activeChannelTab, {[key]: tab});
            // Pre-load download channel messages when switching to Bot IRC tab
            if (tab === 'download' && this.activeServer && this.activeChannel) {
                const dlChan = this.downloadChannelForCurrent();
                if (dlChan && dlChan !== this.activeChannel) {
                    this.loadChannelMessages(this.activeServer, dlChan);
                }
            }
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

        // Returns array of realm objects for a server (from _realmConfigs)
        realmsForServer(serverId) {
            const result = [];
            const seen = {};
            for (const key in this._realmConfigs) {
                const cfg = this._realmConfigs[key];
                if (cfg.server_id === serverId && !seen[cfg.name]) {
                    seen[cfg.name] = true;
                    result.push(cfg);
                }
            }
            result.sort(function(a, b) {
                var na = (a.display_name || a.name).toLowerCase();
                var nb = (b.display_name || b.name).toLowerCase();
                return na < nb ? -1 : na > nb ? 1 : 0;
            });
            return result;
        },

        // Display name for a realm channel
        realmDisplayName(serverId, channelName) {
            const cfg = this._realmConfigs[this.channelKey(serverId, channelName)];
            return cfg ? (cfg.display_name || cfg.name) : channelName;
        },

        selectRealm(serverId, channelName) {
            this.selectChannel(serverId, channelName);
            const realm = this._realmConfigs[this.channelKey(serverId, channelName)];
            api.joinChannel(serverId, channelName)
                .then(() => setTimeout(() => {
                    this.loadChannelMessages(serverId, channelName);
                    this.loadUserList(serverId, channelName);
                }, 1500))
                .catch(console.error);
            if (realm && realm.download_channel && realm.download_channel !== channelName) {
                api.joinChannel(serverId, realm.download_channel)
                    .then(() => setTimeout(() => {
                        this.loadChannelMessages(serverId, realm.download_channel);
                        this.loadUserList(serverId, realm.download_channel);
                    }, 1500))
                    .catch(console.error);
            }
        },

        // Send message to a channel with slash command support
        sendToChannel(serverId, channel) {
            const text = this.ircInput.trim();
            if (!text || !serverId || !channel) return;
            this.ircInput = '';
            if (text.startsWith('/')) {
                api.sendRaw(serverId, text.slice(1)).catch(console.error);
            } else {
                api.sendMessage(serverId, channel, text).catch(console.error);
            }
        },

        // Send raw command to server (for server view input)
        sendToServer(serverId) {
            const text = this.ircInput.trim();
            if (!text || !serverId) return;
            this.ircInput = '';
            if (text.startsWith('/')) {
                api.sendRaw(serverId, text.slice(1)).catch(console.error);
            } else {
                api.sendRaw(serverId, 'PRIVMSG * :' + text).catch(console.error);
            }
        },

        // --- IRC actions ---

        // Add a message to the local channel buffer (client-side echo for sent messages).
        _localEcho(serverId, channel, nick, text) {
            const key = this.channelKey(serverId, channel);
            const existing = this.ircMessages[key] || [];
            const msg = { nick, message: text, timestamp: new Date().toISOString(), msg_type: 'privmsg' };
            this.ircMessages = Object.assign({}, this.ircMessages, { [key]: existing.concat([msg]) });
            this._msgVersion++;
            setTimeout(() => {
                const el = document.querySelector('[x-ref="ircLog"]');
                if (el) el.scrollTop = el.scrollHeight;
            }, 0);
        },

        async sendIrc() {
            const text = this.ircInput.trim();
            if (!text || !this.activeServer) return;
            this.ircInput = '';
            if (text.startsWith('/')) {
                await api.sendRaw(this.activeServer, text.slice(1));
            } else {
                if (!this.activeChannel) return;
                // Intercept search command (e.g. "!s query")
                const realmCfg = this._realmConfigs[this.channelKey(this.activeServer, this.activeChannel)];
                const searchCmd = (realmCfg && realmCfg.search_command) ? realmCfg.search_command : null;
                if (searchCmd && text.startsWith(searchCmd + ' ')) {
                    const query = text.slice(searchCmd.length + 1).trim();
                    if (query) {
                        const srv = this.servers.find(function(s) { return s.id === this.activeServer; }.bind(this));
                        this._localEcho(this.activeServer, this.activeChannel, (srv && srv.nickname) || 'me', text);
                        this.searchQuery = query;
                        this.setActiveTab(this.channelKey(this.activeServer, this.activeChannel), 'search');
                        this.runGlobalSearch();
                        return;
                    }
                }
                const srv = this.servers.find(function(s) { return s.id === this.activeServer; }.bind(this));
                this._localEcho(this.activeServer, this.activeChannel, (srv && srv.nickname) || 'me', text);
                await api.sendMessage(this.activeServer, this.activeChannel, text);
            }
        },

        // --- Search ---

        async runSearch() {
            if (!this.activeServer || !this.activeChannel || !this.searchQuery.trim()) return;
            const token = ++this.searchToken;
            this.searchRunning = true;
            this.searchBotWarning = null;
            this.searchResults = [];
            this.selectedUser = null;
            this._searchDisplayLimit = 200;
            const searchStarted = Date.now();
            this._searchSince = searchStarted;
            try {
                var searchBot = null;
                var searchTimeout = 10;
                try {
                    var startRes = await api.startSearch(this.activeServer, this.activeChannel, this.searchQuery);
                    searchBot = startRes && startRes.search_bot;
                    searchTimeout = (startRes && startRes.search_timeout > 0) ? startRes.search_timeout : 10;
                } catch(e) {
                    console.error('startSearch failed', e);
                }
                var results = [];
                var maxAttempts = Math.max(searchTimeout * 2, 30);
                for (var attempt = 0; attempt < maxAttempts; attempt++) {
                    await new Promise(function(resolve) { setTimeout(resolve, 1000); });
                    if (token !== this.searchToken) {
                        return;
                    }
                    var res = await api.getSearchResults(this.searchQuery, this.activeServer, this.activeChannel);
                    results = Array.isArray(res) ? res : [];
                    if (searchBot && !this.searchBotWarning && (Date.now() - searchStarted > searchTimeout * 1000) && results.length === 0) {
                        this.searchBotWarning = 'No response from ' + searchBot + ' after ' + searchTimeout + 's — check bot name in realm settings';
                    }
                    if (results.length > 0) break;
                }
                if (token !== this.searchToken) {
                    return;
                }
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
                    name: this.searchQuery.trim(),
                    query: this.searchQuery.trim(),
                    server_id: this.activeServer || 0,
                    channel: this.activeChannel || '',
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
            this.searchBotWarning = null;
            this.searchResults = [];
            this.selectedUser = null;
            this._searchDisplayLimit = 200;
            const searchStarted = Date.now();
            this._searchSince = searchStarted;
            const targets = [];
            var searchBot = null;
            var searchTimeout = 10;
            try {
                for (var i = 0; i < this.servers.length; i++) {
                    var srv = this.servers[i];
                    if (this.globalSearchServerId && srv.id !== parseInt(this.globalSearchServerId)) continue;
                    var searchChannels = this.realms
                        .filter(function(realm) { return realm.server_id === srv.id && realm.enabled; })
                        .map(function(realm) { return realm.name; });
                    var channels = searchChannels.length > 0 ? searchChannels : (srv.channels || []);
                    for (var j = 0; j < channels.length; j++) {
                        targets.push({ server_id: srv.id, channel: channels[j] });
                    }
                }
                if (targets.length === 0) {
                    this.searchBotWarning = this.servers.length === 0
                        ? 'No servers configured.'
                        : 'No search channels configured — add a Realm with a search bot in Settings.';
                    return;
                }
                var startOk = 0;
                var startFail = 0;
                var startTotal = targets.length;
                for (var k = 0; k < targets.length; k++) {
                    api.startSearch(targets[k].server_id, targets[k].channel, this.searchQuery).then(function(r) {
                        startOk++;
                        if (!searchBot && r && r.search_bot) {
                            searchBot = r.search_bot;
                            searchTimeout = (r && r.search_timeout > 0) ? r.search_timeout : 10;
                        }
                    }).catch(function(e) {
                        startFail++;
                        console.error('startSearch failed for target', e);
                    });
                }
                var stableCount = 0;
                var lastCount = 0;
                for (var attempt = 0; attempt < 60; attempt++) {
                    await new Promise(function(resolve) { setTimeout(resolve, 1000); });
                    if (token !== this.searchToken) {
                        return;
                    }
                    if (!this.searchBotWarning && startFail > 0 && startOk + startFail >= startTotal && startOk === 0) {
                        this.searchBotWarning = 'Search failed — IRC may not be connected. Check server status.';
                    }
                    var res = await api.getAllSearchResults(this.searchQuery, searchStarted);
                    var count = Array.isArray(res) ? res.length : 0;
                    if (Array.isArray(res) && res.length > 0) {
                        this.searchResults = res;
                        if (res.length === lastCount) {
                            stableCount++;
                            if (stableCount >= 10) break;
                        } else {
                            stableCount = 0;
                            lastCount = res.length;
                        }
                    }
                    if (searchBot && !this.searchBotWarning && (Date.now() - searchStarted > searchTimeout * 1000) && this.searchResults.length === 0) {
                        this.searchBotWarning = 'No response from ' + searchBot + ' after ' + searchTimeout + 's — check bot name in realm settings';
                    }
                }
                if (token === this.searchToken && this.searchResults.length === 0) {
                    var cached = await api.getAllSearchResults(this.searchQuery, null);
                    if (Array.isArray(cached) && cached.length > 0) {
                        this.searchResults = cached;
                    } else {
                    }
                } else if (token === this.searchToken) {
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

        displaySearchRows() {
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
            return arr.slice(0, this._searchDisplayLimit).map(function(r) { return normalizeSearchRow(r); });
        },

        sortedResults() { return this.displaySearchRows(); },

        loadMoreResults() {
            this._searchDisplayLimit += 200;
        },

        downloadPack(row) {
            const key = (row.server_id || this.activeServer) + ':' + (row.bot_nick || '') + ':' + (row.pack_number || '');
            this._downloadingKeys = Object.assign({}, this._downloadingKeys, { [key]: 'queuing' });
            // Parse human-readable filesize (e.g. "6.1G", "204M") to bytes
            var filesizeBytes = 0;
            if (typeof row.filesize === 'number') {
                filesizeBytes = row.filesize;
            } else if (typeof row.filesize === 'string') {
                var match = row.filesize.match(/^([\d.]+)\s*([KMGTP]?)i?[Bb]?$/i);
                if (match) {
                    var val = parseFloat(match[1]);
                    var unit = (match[2] || '').toUpperCase();
                    var multipliers = { '': 1, 'K': 1024, 'M': 1024*1024, 'G': 1024*1024*1024, 'T': 1024*1024*1024*1024 };
                    filesizeBytes = Math.round(val * (multipliers[unit] || 1));
                }
            }
            api.requestDownload({
                server_id: row.server_id || this.activeServer,
                channel: row.channel || this.activeChannel || '',
                bot_nick: row.bot_nick,
                pack_number: row.pack_number,
                filename: row.filename || '',
                filesize: filesizeBytes,
                stats_only: this.statsOnlyDefault,
            }).then(() => {
                this._downloadingKeys = Object.assign({}, this._downloadingKeys, { [key]: 'queued' });
                this.loadDownloads();
                // Reset button label after 3 seconds
                setTimeout(() => {
                    const updated = Object.assign({}, this._downloadingKeys);
                    delete updated[key];
                    this._downloadingKeys = updated;
                }, 3000);
            }).catch((e) => {
                console.error('download request error', e);
                const updated = Object.assign({}, this._downloadingKeys);
                delete updated[key];
                this._downloadingKeys = updated;
            });
        },

        downloadPackLabel(row) {
            const key = (row.server_id || this.activeServer) + ':' + (row.bot_nick || '') + ':' + (row.pack_number || '');
            const state = this._downloadingKeys && this._downloadingKeys[key];
            if (state === 'queuing') return 'Queuing…';
            if (state === 'queued') return '✓ Queued';
            return 'Download';
        },

        isDownloadPending(row) {
            const key = (row.server_id || this.activeServer) + ':' + (row.bot_nick || '') + ':' + (row.pack_number || '');
            return !!(this._downloadingKeys && this._downloadingKeys[key]);
        },

        clearChannelMessages() {
            if (!this.activeServer || !this.activeChannel) return;
            const key = this.channelKey(this.activeServer, this.activeChannel);
            this.ircMessages = Object.assign({}, this.ircMessages, { [key]: [] });
            this._msgVersion++;
        },

        clearSpecificChannelMessages(serverId, channel) {
            if (!serverId || !channel) return;
            const key = this.channelKey(serverId, channel);
            this.ircMessages = Object.assign({}, this.ircMessages, { [key]: [] });
            this._msgVersion++;
        },

        clearServerMessages(serverId) {
            if (!serverId) return;
            this.serverMessages = Object.assign({}, this.serverMessages, { [serverId]: [] });
            this._msgVersion++;
        },

        clearSearchResults() {
            this.searchToken++;
            this.searchRunning = false;
            this._searchSince = null;
            this.searchBotWarning = null;
            this.searchResults = [];
            this.searchQuery = '';
            this.selectedUser = null;
        },

        teachParser(rawLine) {
            this.patternTrainer = {
                open: true,
                rawLine: rawLine,
                annotations: [],
                fieldPickerVisible: false,
                fieldPickerX: 0,
                fieldPickerY: 0,
                pendingStart: null,
                pendingEnd: null,
                patternName: '',
                previewRegex: '',
                previewFieldMapping: '',
                saving: false,
                savedCount: null,
                error: '',
            };
        },

        closeTrainer() { this.patternTrainer.open = false; },

        trainerLineHtml() {
            const line = this.patternTrainer.rawLine;
            const runes = [...line];
            const anns = this.patternTrainer.annotations.slice()
                .sort((a, b) => a.start - b.start);
            let html = '';
            let pos = 0;
            for (const ann of anns) {
                if (ann.start > pos) {
                    html += escHtml(runes.slice(pos, ann.start).join(''));
                }
                html += `<span style="background:${ann.color};color:#fff;border-radius:3px;padding:0 2px" title="${ann.label}">${escHtml(runes.slice(ann.start, ann.end).join(''))}</span>`;
                pos = ann.end;
            }
            if (pos < runes.length) html += escHtml(runes.slice(pos).join(''));
            return html;
        },

        onTrainerMouseup($event) {
            const sel = window.getSelection();
            if (!sel || sel.isCollapsed) return;
            const container = $event.currentTarget;
            const range = sel.getRangeAt(0);
            // Calculate character offsets within container's text content
            const start = getCharOffset(container, range.startContainer, range.startOffset);
            const end   = getCharOffset(container, range.endContainer,   range.endOffset);
            if (end <= start) { sel.removeAllRanges(); return; }
            // Check for overlap with existing annotations
            const anns = this.patternTrainer.annotations;
            for (const ann of anns) {
                if (start < ann.end && end > ann.start) {
                    sel.removeAllRanges();
                    return; // overlaps existing annotation — ignore
                }
            }
            this.patternTrainer.pendingStart = start;
            this.patternTrainer.pendingEnd   = end;
            // Position field picker near the selection
            const rect = range.getBoundingClientRect();
            this.patternTrainer.fieldPickerX = rect.left + window.scrollX;
            this.patternTrainer.fieldPickerY = rect.bottom + window.scrollY + 6;
            this.patternTrainer.fieldPickerVisible = true;
            sel.removeAllRanges();
        },

        addTrainerAnnotation(field) {
            const f = this._trainerFields.find(x => x.field === field);
            if (!f) return;
            const { pendingStart: start, pendingEnd: end } = this.patternTrainer;
            if (start == null || end == null) return;
            this.patternTrainer.annotations.push({ start, end, field, label: f.label, color: f.color });
            this.patternTrainer.annotations.sort((a, b) => a.start - b.start);
            this.patternTrainer.fieldPickerVisible = false;
            this.patternTrainer.pendingStart = null;
            this.patternTrainer.pendingEnd   = null;
            this.patternTrainer.previewRegex = '';
            this.patternTrainer.savedCount   = null;
            this.patternTrainer.error        = '';
        },

        removeTrainerAnnotation(idx) {
            this.patternTrainer.annotations.splice(idx, 1);
            this.patternTrainer.previewRegex = '';
            this.patternTrainer.savedCount   = null;
        },

        async previewTrainerPattern() {
            const t = this.patternTrainer;
            if (!t.annotations.length) return;
            t.error = '';
            try {
                const res = await api.learnPattern({
                    raw_line: t.rawLine,
                    annotations: t.annotations.map(a => ({ start: a.start, end: a.end, field: a.field })),
                    name: t.patternName || 'custom',
                    preview: true,
                });
                if (res.error) { t.error = res.error; return; }
                t.previewRegex        = res.regex        || '';
                t.previewFieldMapping = res.field_mapping || '';
            } catch (e) {
                t.error = 'Preview failed: ' + e.message;
            }
        },

        async saveTrainerPattern() {
            const t = this.patternTrainer;
            if (!t.annotations.length || !t.patternName.trim()) return;
            t.saving = true;
            t.error  = '';
            try {
                const res = await api.learnPattern({
                    raw_line: t.rawLine,
                    annotations: t.annotations.map(a => ({ start: a.start, end: a.end, field: a.field })),
                    name: t.patternName.trim(),
                    preview: false,
                });
                if (res.error) { t.error = res.error; return; }
                t.savedCount    = res.newly_parsed ?? 0;
                t.previewRegex  = res.regex        || t.previewRegex;
                // Refresh search results so newly-parsed rows appear
                if (this.searchQuery) {
                    const res2 = await api.getAllSearchResults(this.searchQuery, this._searchSince);
                    if (Array.isArray(res2) && res2.length > 0) this.searchResults = res2;
                }
            } catch (e) {
                t.error = 'Save failed: ' + e.message;
            } finally {
                t.saving = false;
            }
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

        dlSpeedInfo(dl) {
            if (dl.status === 'downloading') return dl.speed ? formatSpeed(dl.speed) : '-';
            if (dl.status === 'completed') return dl.average_speed ? formatSpeed(dl.average_speed) : '-';
            if (dl.status === 'failed' || dl.status === 'needs_action') {
                var msg = dl.error_message || '';
                return msg.length > 40 ? msg.slice(0, 37) + '...' : (msg || '-');
            }
            return '-';
        },

        dlTimeInfo(dl) {
            if (dl.status === 'downloading') {
                return dl.speed ? formatETA(Math.max(0, (dl.filesize || dl.total_size || 0) - (dl.downloaded_bytes || dl.bytes_received || 0)), dl.speed) : '-';
            }
            if (dl.status === 'completed') {
                return formatDuration(dl.started_at, dl.completed_at);
            }
            var ts = dl.created_at;
            var rel = formatRelativeTime(ts);
            return rel || this.fmtDate(ts);
        },

        dlTimeTooltip(dl) {
            var parts = [];
            if (dl.created_at) parts.push('Added: ' + this.fmtDate(dl.created_at));
            if (dl.started_at) parts.push('Started: ' + this.fmtDate(dl.started_at));
            if (dl.completed_at) parts.push('Finished: ' + this.fmtDate(dl.completed_at));
            return parts.join('\n');
        },

        dlIsClearable(dl) {
            return dl.status === 'completed' || dl.status === 'failed' || dl.status === 'cancelled' || dl.status === 'needs_action';
        },

        toggleDlSelect(id) {
            var sel = Object.assign({}, this._dlSelected);
            if (sel[id]) { delete sel[id]; } else { sel[id] = true; }
            this._dlSelected = sel;
            this._dlSelectAll = this.filteredDownloads().filter(d => this.dlIsClearable(d)).every(d => sel[d.id]);
        },

        toggleDlSelectAll() {
            this._dlSelectAll = !this._dlSelectAll;
            var sel = {};
            if (this._dlSelectAll) {
                this.filteredDownloads().filter(d => this.dlIsClearable(d)).forEach(d => { sel[d.id] = true; });
            }
            this._dlSelected = sel;
        },

        dlSelectedCount() {
            return Object.keys(this._dlSelected).length;
        },

        dlCountByStatus(status) {
            if (status === 'failed') return this.downloads.filter(d => d.status === 'failed' || d.status === 'needs_action').length;
            return this.downloads.filter(d => d.status === status).length;
        },

        async dlClearByStatus(status) {
            if (this._dlConfirm !== 'status:' + status) {
                this._dlConfirm = 'status:' + status;
                clearTimeout(this._dlConfirmTimer);
                this._dlConfirmTimer = setTimeout(() => { this._dlConfirm = null; }, 3000);
                return;
            }
            this._dlConfirm = null;
            try {
                await api.clearDownloads(status);
                if (status === 'failed') await api.clearDownloads('needs_action');
                this._dlSelected = {};
                await this.loadDownloads();
            } catch (e) { console.error('clearDownloads error', e); }
        },

        async dlClearSelected() {
            if (this._dlConfirm !== 'selected') {
                this._dlConfirm = 'selected';
                clearTimeout(this._dlConfirmTimer);
                this._dlConfirmTimer = setTimeout(() => { this._dlConfirm = null; }, 3000);
                return;
            }
            this._dlConfirm = null;
            var ids = Object.keys(this._dlSelected).map(Number);
            if (ids.length === 0) return;
            try {
                await api.deleteDownloads(ids);
                this._dlSelected = {};
                this._dlSelectAll = false;
                await this.loadDownloads();
            } catch (e) { console.error('deleteDownloads error', e); }
        },

        fmtDate(isoString) {
            return formatDateCustom(isoString, this.dateFormat);
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
                    const existing = this.ircMessages[key] || [];
                    const msgText = data.message || (data.data && data.data.message) || '';
                    const msgType = (data.data && data.data.type) || 'privmsg';
                    // Skip raw lines that produce no visible output (NAMES list, end of NAMES, topic-setter timestamp)
                    if (msgType === 'raw' && formatRawLine(msgText) === '') return;
                    const newMsg = {
                        nick: data.nick,
                        message: msgText,
                        timestamp: data.timestamp,
                        msg_type: msgType,
                    };
                    let msgs = existing.concat([newMsg]);
                    if (msgs.length > 500) msgs = msgs.slice(msgs.length - 500);
                    this.ircMessages = Object.assign({}, this.ircMessages, { [key]: msgs });
                    this._msgVersion++;
                    // Auto-scroll if this message is for the active channel
                    if (data.server_id === this.activeServer && data.channel === this.activeChannel) {
                        setTimeout(() => {
                            const el = document.querySelector('[x-ref="ircLog"]');
                            if (el) el.scrollTop = el.scrollHeight;
                        }, 0);
                    }
                    // Auto-scroll download pane if this is the download channel
                    if (data.server_id === this.activeServer && this.activeChannel) {
                        const dlChan = this._channelConfigs[this.channelKey(this.activeServer, this.activeChannel)];
                        if (dlChan && dlChan !== this.activeChannel && data.channel === dlChan) {
                            setTimeout(() => {
                                const el = document.querySelector('[x-ref="ircLogDownload"]');
                                if (el) el.scrollTop = el.scrollHeight;
                            }, 0);
                        }
                    }
                } else {
                    // Server-level message (no channel): buffer in serverMessages only
                    const sid = data.server_id;
                    const existing = this.serverMessages[sid] || [];
                    const newMsg = {
                        nick: data.nick,
                        message: data.message || (data.data && data.data.message) || '',
                        timestamp: data.timestamp,
                        msg_type: (data.data && data.data.type) || 'raw',
                    };
                    let msgs = existing.concat([newMsg]);
                    if (msgs.length > 500) msgs = msgs.slice(msgs.length - 500);
                    this.serverMessages = Object.assign({}, this.serverMessages, { [sid]: msgs });
                    this._msgVersion++;
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
            } else if (type === 'search_result') {
                // Real-time push from parser — immediately refresh results if search is running
                const d = data.data;
                if (d && d.parsed && d.search_query === this.searchQuery && this.searchRunning && this._searchSince) {
                    // Use channel-scoped fetch for channel search, global for global search
                    var refreshPromise;
                    if (this.activeServer && this.activeChannel) {
                        refreshPromise = api.getSearchResults(this.searchQuery, this.activeServer, this.activeChannel);
                    } else {
                        refreshPromise = api.getAllSearchResults(this.searchQuery, this._searchSince);
                    }
                    refreshPromise.then((res) => {
                        if (Array.isArray(res) && res.length > 0) this.searchResults = res;
                    }).catch(() => {});
                }
            } else if (type === 'search_bot_detected') {
                // Auto-detection saved a search bot — update local realm config
                const d = data.data;
                if (d && d.realm_id && d.bot_nick) {
                    // Update _realmConfigs
                    for (const key in this._realmConfigs) {
                        if (this._realmConfigs[key].id === parseInt(d.realm_id)) {
                            this._realmConfigs[key].search_bot = d.bot_nick;
                            break;
                        }
                    }
                    // Refresh settings if viewing realms
                    if (this.settingsServerId) {
                        this.loadSettingsRealms(this.settingsServerId);
                    }
                }
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
            return api.getRealms(serverId).then(r => { this.settingsChannels = Array.isArray(r) ? r : []; this.settingsRealms = this.settingsChannels; });
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
                : { id: null, server_id: this.settingsServerId, name: '', search_command: '', download_channel: '', search_bot: '', search_timeout: 10, auto_join: false, enabled: true };
            this.showChannelForm = true;
        },

        saveChannel() {
            const p = this.channelForm.id
                ? api.updateRealm(this.channelForm.id, this.channelForm)
                : api.createRealm(this.channelForm);
            return p.then(async () => {
                this.showChannelForm = false;
                await this.loadSettingsChannels(this.settingsServerId);
                // Refresh the nav channel list from IRC status
                try {
                    const res = await api.getIRCStatus();
                    const statuses = Array.isArray(res) ? res : [];
                    const statusMap = {};
                    for (const s of statuses) {
                        statusMap[s.server_id] = s;
                    }
                    this.servers = this.servers.map(srv => {
                        const st = statusMap[srv.id];
                        return Object.assign({}, srv, {
                            channels: (st && st.channels) ? st.channels : (srv.channels || []),
                        });
                    });
                } catch (e) {
                    console.error('refreshIRCStatus after saveChannel', e);
                }
            }).catch(console.error);
        },

        deleteChannel(id) {
            if (!confirm('Delete channel?')) return;
            return api.deleteRealm(id).then(() => this.loadSettingsChannels(this.settingsServerId)).catch(console.error);
        },

        loadSettingsRealms(serverId) {
            this.settingsServerId = serverId;
            return api.getRealms(serverId).then(r => { this.settingsRealms = Array.isArray(r) ? r : []; });
        },

        openRealmForm(realm) {
            this.realmForm = realm
                ? Object.assign({}, realm)
                : { id: null, server_id: this.settingsServerId, name: '', display_name: '', search_command: '', download_channel: '', search_bot: '', search_timeout: 10, auto_join: false, enabled: true };
            this.showRealmForm = true;
        },

        formatIrcMessage(text) {
            if (typeof ircColors !== 'undefined' && ircColors.toHtml) {
                return ircColors.toHtml(text);
            }
            return this.escapeHtml(text);
        },

        escapeHtml(str) {
            var div = document.createElement('div');
            div.textContent = str;
            return div.innerHTML;
        },

        saveRealm() {
            const p = this.realmForm.id
                ? api.updateRealm(this.realmForm.id, this.realmForm)
                : api.createRealm(this.realmForm);
            return p.then(async () => {
                this.showRealmForm = false;
                await this.loadSettingsRealms(this.settingsServerId);
                // Re-load realm configs
                const realms = await api.getRealms(this.settingsServerId).catch(() => []);
                for (const r of (realms || [])) {
                    const key = this.channelKey(this.settingsServerId, r.name);
                    this._realmConfigs[key] = Object.assign({}, r, { server_id: this.settingsServerId });
                    this._channelConfigs[key] = r.download_channel || r.name;
                }
            }).catch(console.error);
        },

        deleteRealm(id) {
            if (!confirm('Delete realm?')) return;
            return api.deleteRealm(id).then(() => this.loadSettingsRealms(this.settingsServerId)).catch(console.error);
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

            // Load realm configs (for download_channel and display name mapping)
            this.realms = [];
            for (const srv of this.servers) {
                const realms = await api.getRealms(srv.id).catch(() => []);
                for (const r of (realms || [])) {
                    this.realms.push(Object.assign({}, r, { server_id: srv.id }));
                    const key = this.channelKey(srv.id, r.name);
                    this._channelConfigs[key] = r.download_channel || r.name;
                    this._realmConfigs[key] = Object.assign({}, r, { server_id: srv.id });
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
