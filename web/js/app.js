function escHtml(s) {
    return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// --- File manager icons ---

const FM_ICON_CATEGORIES = {
    video: ['mkv', 'mp4', 'avi', 'mov', 'wmv', 'webm', 'm4v', 'ts'],
    audio: ['mp3', 'flac', 'm4a', 'ogg', 'opus', 'wav', 'aac'],
    subtitle: ['srt', 'sub', 'ass', 'ssa', 'txt', 'nfo'],
    book: ['pdf', 'epub', 'mobi', 'azw3', 'cbz', 'cbr'],
    archive: ['zip', 'rar', '7z', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'zst'],
    disc: ['iso', 'img'],
    image: ['jpg', 'jpeg', 'png', 'gif', 'webp'],
    app: ['exe', 'msi', 'dmg', 'pkg', 'deb', 'rpm', 'appimage'],
};

const FM_ICON_SVG = {
    folder: '<path d="M2 4a1 1 0 0 1 1-1h3.3l1.4 1.5H13a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4z"/>',
    video: '<rect x="2" y="3" width="12" height="10" rx="1"/><path d="M2 6h12M2 10h12M5.5 3v3M5.5 10v3M10.5 3v3M10.5 10v3"/>',
    audio: '<circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="10.5" r="1.6"/><path d="M6.6 12V4l7-1.3v7.3"/>',
    subtitle: '<path d="M4 2h6l3 3v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1z"/><path d="M10 2v3h3"/><path d="M5 9h6M5 11.5h4"/>',
    book: '<path d="M3 3.4C3 2.6 3.7 2 4.5 2H8v11H4.5C3.7 13 3 13.6 3 14.4V3.4z"/><path d="M13 3.4c0-.8-.7-1.4-1.5-1.4H8v11h3.5c.8 0 1.5.6 1.5 1.4V3.4z"/>',
    archive: '<rect x="2.5" y="3" width="11" height="10" rx="1"/><path d="M8 3v1.4M8 6v1.2M8 8.6v1.2M8 11.2v1.8"/>',
    disc: '<circle cx="8" cy="8" r="6"/><circle cx="8" cy="8" r="1.4"/>',
    image: '<rect x="2" y="3" width="12" height="10" rx="1"/><circle cx="6" cy="6.5" r="1.2"/><path d="M2 11l3.5-3.5L8 10l2.5-2.5L14 11"/>',
    app: '<rect x="2.5" y="2.5" width="11" height="11" rx="2.5"/><path d="M6.5 5.5l4 2.5-4 2.5z"/>',
    generic: '<path d="M4 2h5l4 4v8a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1z"/><path d="M9 2v4h4"/>',
};

function fmExt(name) {
    var i = (name || '').lastIndexOf('.');
    return i > 0 ? name.slice(i + 1).toLowerCase() : '';
}

function fmCategory(entry) {
    if (entry.is_dir) return 'folder';
    var ext = fmExt(entry.name);
    for (var cat in FM_ICON_CATEGORIES) {
        if (FM_ICON_CATEGORIES[cat].indexOf(ext) !== -1) return cat;
    }
    return 'generic';
}

function fileIcon(entry) {
    var cat = fmCategory(entry);
    var svg = FM_ICON_SVG[cat] || FM_ICON_SVG.generic;
    return '<svg class="fm-icon fm-icon-' + cat + '" width="16" height="16" viewBox="0 0 16 16" ' +
        'fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">' +
        svg + '</svg>';
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
        realmSearchQuery: '',
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
        searchTargets: [],
        searchViewMode: localStorage.getItem('mxirc_searchViewMode') || 'merged',
        downloads: [],
        downloadFilter: 'all',
        downloadSearch: '',
        dlSort: { col: 'created_at', dir: 'desc' },
        downloadTargets: [],
        _dlTargetsLoaded: false,
        _dlPredicted: {},       // filename -> predicted path (from /library/preview)
        _dlMediaRoot: '',
        _dlMediaRootLoaded: false,
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

        // Library (auto-organize) settings
        library: null,          // {auto_organize, search_depth, media_root, categories:[]}
        libraryKinds: {},       // {kind: [fieldName]}
        _libraryOriginal: null, // JSON snapshot for Reload
        libraryError: '',
        librarySaved: false,
        libraryNote: '',
        libraryPreviewName: '',
        libraryPreviewResult: null,
        _libraryPreviewTimer: null,

        // Mode
        appMode: localStorage.getItem('mxirc_mode') || 'simple',
        dateFormat: localStorage.getItem('mxirc_date_format') || 'DD-MM-YYYY HH:MM:SS',

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
        fileManagerRoot: '',
        fileManagerEntries: [],
        fileManagerLoading: false,
        fileManagerParent: '',
        fileManagerError: '',
        fileManagerErrors: {},
        fileManagerFilter: '',
        fileManagerSort: { col: 'name', dir: 'asc' },
        fileManagerSelected: {},
        fileManagerSelectAll: false,
        fileManagerRename: { name: null, value: '' },
        fileManagerNewFolderOpen: false,
        fileManagerNewFolderName: '',
        fileManagerDragOverPath: null,
        _fmConfirm: null,
        _fmConfirmTimer: null,
        _fmDragNames: null,

        // Errors
        errors: [],
        unreadErrors: 0,

        // Stats
        statsSummary: null,
        statsHistory: [],
        statsHistoryOffset: 0,
        statsLoading: false,
        indexDetail: null,

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

        // Pattern settings
        patternSettingsList: [],
        patternScopeOptions: [],   // [{label, server_id, channel}] built from all servers+realms
        patternTagEdit: { id: null, value: '' },
        patternImportMsg: '',
        patternImportError: false,
        patternTesterLine: '',
        patternTesterResult: null,
        exportPatternsDialog: { open: false, json: '', copied: false },

        // Global search filter (advanced mode)
        globalSearchServerId: '',

        // Search mode: 'live' hits IRC bots; 'index' searches the persistent,
        // self-collected catalog built from past live searches (instant, offline).
        searchMode: 'index',
        indexStats: { total_files: 0 },

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
                this._dlTargetsLoaded = false; // move targets may have changed on disk
                this.loadDownloadTargets();
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

        expandAllServers() {
            for (const srv of this.servers) this.expandedServers[srv.id] = true;
        },

        collapseAllServers() {
            for (const srv of this.servers) this.expandedServers[srv.id] = false;
        },

        isExpanded(serverId) {
            return !!this.expandedServers[serverId];
        },

        setMode(mode) {
            this.appMode = mode;
            localStorage.setItem('mxirc_mode', mode);
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
            localStorage.setItem('mxirc_layout_' + key, layout);
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

        reconnectServer(serverId) {
            api.connectServer(serverId).catch(e => console.warn('reconnect failed', e));
        },

        rejoinChannel(serverId, channel) {
            api.joinChannel(serverId, channel).catch(e => console.warn('rejoin failed', e));
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
            if (!this.activeServer || !this.activeChannel || !this.realmSearchQuery.trim()) return;
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
                    var startRes = await api.startSearch(this.activeServer, this.activeChannel, this.realmSearchQuery);
                    searchBot = startRes && startRes.search_bot;
                    searchTimeout = (startRes && startRes.search_timeout > 0) ? startRes.search_timeout : 10;
                    // Local echo: inject the sent command as an outgoing message in the channel
                    const realmCfgEcho = this._realmConfigs[this.channelKey(this.activeServer, this.activeChannel)];
                    const echoCmd = ((realmCfgEcho && realmCfgEcho.search_command) || '!s') + ' ' + this.realmSearchQuery;
                    const echoKey = this.channelKey(this.activeServer, this.activeChannel);
                    const echoMsgs = (this.ircMessages[echoKey] || []).concat([{
                        nick: 'you',
                        message: echoCmd,
                        timestamp: new Date().toISOString(),
                        msg_type: 'privmsg',
                        local: true,
                    }]);
                    this.ircMessages = Object.assign({}, this.ircMessages, { [echoKey]: echoMsgs });
                    this._msgVersion++;
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
                    var res = await api.getSearchResults(this.realmSearchQuery, this.activeServer, this.activeChannel);
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
            // In realm context use realmSearchQuery; otherwise use global searchQuery
            const query = (this.activeChannel && this.realmSearchQuery.trim()) ? this.realmSearchQuery.trim() : this.searchQuery.trim();
            if (!query) return;
            try {
                await api.createSavedSearch({
                    name: query,
                    query: query,
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
            this.searchTargets = [];
            this.selectedUser = null;
            this._searchDisplayLimit = 200;
            const searchStarted = Date.now();
            this._searchSince = searchStarted;
            var searchTimeout = 10;
            try {
                for (var i = 0; i < this.servers.length; i++) {
                    var srv = this.servers[i];
                    if (this.globalSearchServerId && srv.id !== parseInt(this.globalSearchServerId)) continue;
                    var searchRealms = this.realms
                        .filter(function(realm) { return realm.server_id === srv.id && realm.enabled; });
                    var searchChannels = searchRealms.map(function(realm) { return realm.name; });
                    var channels = searchChannels.length > 0 ? searchChannels : (srv.channels || []);
                    for (var j = 0; j < channels.length; j++) {
                        // Look up realm display name for this channel
                        var realmCfg = this._realmConfigs[this.channelKey(srv.id, channels[j])];
                        var displayLabel = (realmCfg && (realmCfg.display_name || realmCfg.name)) || srv.name;
                        this.searchTargets.push({
                            server_id: srv.id,
                            server_name: srv.name,
                            display_label: displayLabel,
                            channel: channels[j],
                            status: 'pending',
                            resultCount: 0,
                            unparsedCount: 0,
                            error: null,
                            search_bot: null,
                            search_timeout: 10
                        });
                    }
                }
                if (this.searchTargets.length === 0) {
                    this.searchBotWarning = this.servers.length === 0
                        ? 'No servers configured.'
                        : 'No search channels configured — add a Realm with a search bot in Settings.';
                    return;
                }
                var startOk = 0;
                var startFail = 0;
                var startTotal = this.searchTargets.length;
                for (let k = 0; k < this.searchTargets.length; k++) {
                    this.searchTargets[k].status = 'searching';
                    api.startSearch(this.searchTargets[k].server_id, this.searchTargets[k].channel, this.searchQuery)
                        .then((r) => {
                            startOk++;
                            this.searchTargets[k].status = 'searching'; // confirmed started
                            if (r && r.search_bot) {
                                this.searchTargets[k].search_bot = r.search_bot;
                            }
                            if (r && r.search_timeout > 0) {
                                this.searchTargets[k].search_timeout = r.search_timeout;
                                if (r.search_timeout > searchTimeout) searchTimeout = r.search_timeout;
                            }
                            // Local echo: inject sent command into the channel's message list
                            const tgt = this.searchTargets[k];
                            const echoCfg = this._realmConfigs[this.channelKey(tgt.server_id, tgt.channel)];
                            const echoCmd = ((echoCfg && echoCfg.search_command) || '!s') + ' ' + this.searchQuery;
                            const echoKey = this.channelKey(tgt.server_id, tgt.channel);
                            const echoMsgs = (this.ircMessages[echoKey] || []).concat([{
                                nick: 'you',
                                message: echoCmd,
                                timestamp: new Date().toISOString(),
                                msg_type: 'privmsg',
                                local: true,
                            }]);
                            this.ircMessages = Object.assign({}, this.ircMessages, { [echoKey]: echoMsgs });
                            this._msgVersion++;
                        })
                        .catch((e) => {
                            startFail++;
                            this.searchTargets[k].status = 'error';
                            this.searchTargets[k].error = e.message || 'Failed to start search';
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
                        // Update per-target counts
                        for (let t = 0; t < this.searchTargets.length; t++) {
                            var target = this.searchTargets[t];
                            var targetResults = res.filter(function(r) { return r.server_id === target.server_id && r.channel === target.channel; });
                            target.resultCount = targetResults.length;
                            target.unparsedCount = targetResults.filter(function(r) { return !r.parsed; }).length;
                            if (target.status === 'searching' && targetResults.length > 0) {
                                target.status = 'done';
                            }
                        }
                        if (res.length === lastCount) {
                            stableCount++;
                            if (stableCount >= 10) break;
                        } else {
                            stableCount = 0;
                            lastCount = res.length;
                        }
                    }
                    // Per-target timeout: mark each target's status and, only when
                    // EVERY target has timed out or failed AND there are still no
                    // results anywhere, surface a warning listing each silent bot.
                    if (!this.searchBotWarning && (Date.now() - searchStarted > searchTimeout * 1000)) {
                        var silent = [];
                        var active = 0;
                        for (var t = 0; t < this.searchTargets.length; t++) {
                            var tgt = this.searchTargets[t];
                            if (tgt.status === 'error') continue;
                            active++;
                            if (tgt.resultCount === 0 && tgt.search_bot) {
                                if (tgt.status === 'searching') tgt.status = 'timeout';
                                silent.push(tgt.server_name + ':' + tgt.search_bot);
                            }
                        }
                        if (active > 0 && silent.length === active && this.searchResults.length === 0) {
                            this.searchBotWarning = 'No response from ' + silent.join(', ') + ' — check bot names in realm settings';
                        }
                    }
                }
                if (token === this.searchToken && this.searchResults.length === 0) {
                    var cached = await api.getAllSearchResults(this.searchQuery, null);
                    if (Array.isArray(cached) && cached.length > 0) {
                        this.searchResults = cached;
                    }
                }
            } catch (e) {
                console.error('global search error', e);
            } finally {
                if (token === this.searchToken) {
                    this.searchRunning = false;
                    // Mark any still-searching targets as done
                    for (var t = 0; t < this.searchTargets.length; t++) {
                        if (this.searchTargets[t].status === 'searching') {
                            this.searchTargets[t].status = 'done';
                        }
                    }
                }
            }
        },

        getServerName(serverId) {
            for (var i = 0; i < this.servers.length; i++) {
                if (this.servers[i].id === serverId) return this.servers[i].name;
            }
            return 'Server ' + serverId;
        },

        // --- Index search (self-collected, offline) ---

        setSearchMode(mode) {
            if (this.searchMode === mode) return;
            this.searchMode = mode;
            this.searchResults = [];
            this.searchTargets = [];
            this.searchBotWarning = null;
            if (mode === 'index') this.loadIndexStats();
        },

        async runIndexSearch() {
            if (!this.searchQuery.trim()) return;
            const token = ++this.searchToken;
            this.searchRunning = true;
            this.searchBotWarning = null;
            this.searchResults = [];
            this.searchTargets = [];
            try {
                const serverId = this.globalSearchServerId || null;
                const res = await api.searchIndex(this.searchQuery.trim(), serverId, '');
                if (token !== this.searchToken) return;
                this.searchResults = Array.isArray(res) ? res : [];
                if (this.searchResults.length === 0) {
                    this.searchBotWarning = 'No matches in the index yet — try a Live search first to build it up.';
                }
            } catch (e) {
                console.error('runIndexSearch error', e);
                if (token === this.searchToken) {
                    this.searchBotWarning = 'Index search failed: ' + (e.message || 'unknown error');
                }
            } finally {
                if (token === this.searchToken) this.searchRunning = false;
            }
        },

        async loadIndexStats() {
            try {
                const serverId = this.globalSearchServerId || null;
                const stats = await api.getIndexStats(serverId);
                this.indexStats = stats || { total_files: 0 };
            } catch (e) {
                console.error('loadIndexStats error', e);
            }
        },

        async clearSearchIndex() {
            const scopeLabel = this.globalSearchServerId ? 'this server' : 'all servers';
            if (!confirm('Clear the self-collected search index for ' + scopeLabel + '? This cannot be undone.')) return;
            try {
                const serverId = this.globalSearchServerId || null;
                await api.clearIndex(serverId);
                await this.loadIndexStats();
                this.searchResults = [];
            } catch (e) {
                console.error('clearSearchIndex error', e);
            }
        },

        toggleSearchViewMode() {
            this.searchViewMode = this.searchViewMode === 'merged' ? 'grouped' : 'merged';
            localStorage.setItem('mxirc_searchViewMode', this.searchViewMode);
        },

        groupedSearchResults() {
            var groups = {};
            var rows = this.displaySearchRows();
            for (var i = 0; i < rows.length; i++) {
                var r = rows[i];
                var key = (r.server_id || 0) + ':' + (r.channel || '');
                if (!groups[key]) {
                    groups[key] = {
                        server_id: r.server_id,
                        server_name: this.getServerName(r.server_id),
                        channel: r.channel || '-',
                        results: []
                    };
                }
                groups[key].results.push(r);
            }
            // Sort by result count descending
            return Object.values(groups).sort(function(a, b) { return b.results.length - a.results.length; });
        },

        async loadDiskStats() {
            try {
                var res = await api.getStorageStats();
                this.diskStats = Array.isArray(res) ? res : [];
            } catch (e) {
                console.error('disk stats error', e);
            }
        },

        _sortToggle(sort, col) {
            if (sort.col === col) {
                sort.dir = sort.dir === 'asc' ? 'desc' : 'asc';
            } else {
                sort.col = col;
                sort.dir = 'asc';
            }
        },

        _sortArrow(sort, col) {
            if (sort.col !== col) return '';
            return sort.dir === 'asc' ? ' ▲' : ' ▼';
        },

        sortBy(col) { this._sortToggle(this.searchSort, col); },
        sortIndicator(col) { return this._sortArrow(this.searchSort, col); },

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
                var av = searchSortValue(a, col);
                var bv = searchSortValue(b, col);
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

        realmSearchCount() {
            var serverID = this.activeServer;
            var channel = this.activeChannel;
            return this.searchResults.filter(function(r) {
                return r.server_id === serverID && r.channel === channel;
            }).length;
        },

        realmDisplayRows() {
            var serverID = this.activeServer;
            var channel = this.activeChannel;
            var col = this.searchSort.col;
            var dir = this.searchSort.dir;
            var arr = this.searchResults.filter(function(r) {
                return r.server_id === serverID && r.channel === channel;
            });
            if (this.selectedUser) {
                arr = arr.filter(function(r) { return r.bot_nick === this.selectedUser; }.bind(this));
            }
            arr.sort(function(a, b) {
                var av = searchSortValue(a, col), bv = searchSortValue(b, col);
                if (av == null) av = '';
                if (bv == null) bv = '';
                if (typeof av === 'number' && typeof bv === 'number') {
                    return dir === 'asc' ? av - bv : bv - av;
                }
                av = String(av).toLowerCase(); bv = String(bv).toLowerCase();
                var cmp = av < bv ? -1 : av > bv ? 1 : 0;
                return dir === 'asc' ? cmp : -cmp;
            });
            return arr.slice(0, this._searchDisplayLimit).map(function(r) { return normalizeSearchRow(r); });
        },

        loadMoreResults() {
            this._searchDisplayLimit += 200;
        },

        canDownload(row) {
            // Downloadable = we know whom to ask and which pack. Live search
            // rows signal this via `parsed`; index rows have no such field,
            // so check the actual prerequisites.
            return !!(row.bot_nick && row.pack_number != null);
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

        teachParser(result) {
            var rawLine = typeof result === 'string' ? result : result.raw_line;
            this.patternTrainer = {
                open: true,
                rawLine: rawLine,
                serverID: typeof result === 'object' ? result.server_id : null,
                channel: typeof result === 'object' ? result.channel : '',
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
                    server_id: t.serverID || null,
                    channel: t.channel || '',
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

        dlSortBy(col) { this._sortToggle(this.dlSort, col); },
        dlSortIndicator(col) { return this._sortArrow(this.dlSort, col); },

        dlSortValue(dl, col) {
            if (col === 'created_at') return Date.parse(dl.created_at) || 0;
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
            } else if (this.downloadFilter === 'downloading') {
                arr = arr.filter(d => d.status === 'downloading' || d.status === 'processing');
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
            return arr;
        },

        // ponytail: sort split from filteredDownloads so the x-if/toggle callers
        // that only need membership don't pay the n log n sort each render
        sortedDownloads() {
            var arr = this.filteredDownloads();
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

        async loadDownloads() {
            try {
                const res = await api.getDownloads();
                this.downloads = Array.isArray(res) ? res : [];
            } catch (e) {
                console.error('loadDownloads error', e);
            }
            this.loadDownloadTargets();
            this._predictDownloadTargets();
        },

        async loadDownloadTargets() {
            if (this._dlTargetsLoaded) return;
            this._dlTargetsLoaded = true;
            try {
                const res = await api.getDownloadTargets();
                this.downloadTargets = Array.isArray(res) ? res : [];
            } catch (e) {
                console.error('loadDownloadTargets error', e);
                this._dlTargetsLoaded = false;
            }
        },

        // Predicts the "Auto" target path for queued/downloading/processing rows,
        // via one /library/preview call per loadDownloads() (not per WS tick).
        async _predictDownloadTargets() {
            const names = this.downloads
                .filter(d => ['queued', 'downloading', 'processing'].includes(d.status) && d.filename)
                .map(d => d.filename);
            if (!names.length) { this._dlPredicted = {}; return; }
            try {
                if (!this._dlMediaRootLoaded) {
                    this._dlMediaRootLoaded = true;
                    const cfg = await api.getLibrary().catch(() => null);
                    this._dlMediaRoot = (cfg && cfg.media_root) || '';
                }
                const res = await api.previewLibrary(names);
                const map = {};
                (Array.isArray(res) ? res : []).forEach(r => {
                    if (!r.path) return;
                    let p = r.path;
                    if (this._dlMediaRoot && p.startsWith(this._dlMediaRoot)) {
                        p = p.slice(this._dlMediaRoot.length).replace(/^[\/\\]+/, '');
                    }
                    map[r.filename] = p;
                });
                this._dlPredicted = map;
            } catch (e) {
                console.error('previewLibrary error', e);
            }
        },

        dlAutoLabel(dl) {
            const p = dl.filename && this._dlPredicted[dl.filename];
            return p ? 'Auto → ' + p : 'Auto';
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

        toggleAutoExtract(dl, val) {
            dl.auto_extract = val;
            api.setAutoExtract(dl.id, val).catch(console.error);
        },

        setTarget(dl, dir) {
            dl.target_dir = dir;
            api.setDownloadTarget(dl.id, dir).catch(console.error);
        },

        downloadProgress(dl) {
            if (dl.status === 'completed' || dl.status === 'processing') return 100;
            // total_size/bytes_received only exist after a WS progress event;
            // API rows carry filesize/downloaded_bytes instead.
            var total = dl.total_size || dl.filesize;
            if (!total) return 0;
            var got = dl.bytes_received != null ? dl.bytes_received : (dl.downloaded_bytes || 0);
            return Math.round((got / total) * 100);
        },

        isArchive(filename) {
            if (!filename) return false;
            const lower = filename.toLowerCase();
            return ['.tar', '.tar.gz', '.tgz', '.tar.bz2', '.tbz2', '.tar.xz', '.txz', '.tar.zst']
                .some(ext => lower.endsWith(ext));
        },

        dlSpeedInfo(dl) {
            if (dl.status === 'downloading') return dl.speed ? formatSpeed(dl.speed) : '-';
            if (dl.status === 'processing') {
                var phaseText = { moving: 'Moving…', hooks: 'Running hooks…', extracting: 'Extracting…' };
                return phaseText[dl.phase] || 'Finishing…';
            }
            if (dl.status === 'completed') {
                return dl.average_speed ? formatSpeed(dl.average_speed) : '-';
            }
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
            const url = proto + '//' + location.host + (window.XIRC_PREFIX || '') + '/ws';
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
                    // A late progress event must not pull a 'processing'/'completed'
                    // row back to 'downloading' — only a still-queued row flips.
                    if (dl.status === 'queued') {
                        dl.status = 'downloading';
                    }
                } else {
                    // Download started before our list was loaded — reload to pick it up
                    this.loadDownloads();
                }
            } else if (type === 'download_status') {
                const sd = data.data || {};
                if (sd.status === 'processing') {
                    const dl = this.downloads.find(d => d.id === sd.download_id);
                    if (dl) {
                        dl.status = 'processing';
                        dl.phase = sd.phase;
                    } else {
                        this.loadDownloads();
                    }
                } else if (sd.status === 'completed' || sd.status === 'failed' || sd.status === 'cancelled' || sd.status === 'needs_action') {
                    this.loadDownloads();
                }
            } else if (type === 'realm_updated') {
                const d = data.data;
                if (d && d.field === 'download_channel' && d.value && data.channel) {
                    const key = this.channelKey(data.server_id, data.channel);
                    if (this._realmConfigs[key]) {
                        this._realmConfigs[key].download_channel = d.value;
                    }
                    this._channelConfigs[key] = d.value;
                    api.joinChannel(data.server_id, d.value).catch(() => {});
                }
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
                if (d && d.parsed && (d.search_query === this.searchQuery || d.search_query === this.realmSearchQuery) && this.searchRunning && this._searchSince) {
                    // Use channel-scoped fetch for channel search, global for global search
                    var refreshPromise;
                    if (this.activeServer && this.activeChannel) {
                        refreshPromise = api.getSearchResults(this.realmSearchQuery, this.activeServer, this.activeChannel);
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
            this.fileManagerError = '';
            this.fileManagerErrors = {};
            this.fileManagerFilter = '';
            this.fileManagerSelected = {};
            this.fileManagerSelectAll = false;
            this.fileManagerRename = { name: null, value: '' };
            this.fmCancelNewFolder();
            this.fileManagerDragOverPath = null;
            try {
                const res = await api.listFiles(dir);
                this.fileManagerEntries = (res && res.entries) || [];
                this.fileManagerRoot = (res && res.root) || dir;
                this.fileManagerParent = (res && res.parent) || '';
                this.fileManagerDir = (res && res.dir) || dir;
            } catch(e) {
                console.error('loadFileManager', e);
                this.fileManagerError = e.message || String(e);
            } finally {
                this.fileManagerLoading = false;
            }
        },

        fmJoin(dir, name) {
            return dir.replace(/\/+$/, '') + '/' + name;
        },

        // Breadcrumb segments from the destination root down to the current dir.
        fmBreadcrumb() {
            var root = this.fileManagerRoot || this.fileManagerDir || '';
            var dir = this.fileManagerDir || '';
            var crumbs = [{ name: root, path: root }];
            if (dir && dir !== root && dir.indexOf(root) === 0) {
                var rest = dir.slice(root.length).replace(/^\/+/, '');
                var acc = root;
                if (rest) {
                    rest.split('/').forEach(p => {
                        if (!p) return;
                        acc = this.fmJoin(acc, p);
                        crumbs.push({ name: p, path: acc });
                    });
                }
            }
            return crumbs;
        },

        fmOpenFolder(entry) {
            if (!entry.is_dir) return;
            this.loadFileManager(this.fmJoin(this.fileManagerDir, entry.name));
        },

        fmSortBy(col) { this._sortToggle(this.fileManagerSort, col); },
        fmSortIndicator(col) { return this._sortArrow(this.fileManagerSort, col); },

        fmSizeLabel(entry) {
            if (entry.is_dir) return (entry.items || 0) + ' item' + (entry.items === 1 ? '' : 's');
            return formatSize(entry.size || 0);
        },

        // Filtered + sorted rows, folders always first.
        fmDisplayEntries() {
            var filter = (this.fileManagerFilter || '').toLowerCase();
            var arr = this.fileManagerEntries.filter(e => !filter || e.name.toLowerCase().indexOf(filter) !== -1);
            var col = this.fileManagerSort.col;
            var dir = this.fileManagerSort.dir === 'asc' ? 1 : -1;
            arr = arr.slice().sort((a, b) => {
                if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
                var av, bv;
                if (col === 'size') {
                    av = a.is_dir ? (a.items || 0) : (a.size || 0);
                    bv = b.is_dir ? (b.items || 0) : (b.size || 0);
                } else if (col === 'modified') {
                    av = a.modified || '';
                    bv = b.modified || '';
                } else {
                    av = (a.name || '').toLowerCase();
                    bv = (b.name || '').toLowerCase();
                }
                if (av < bv) return -1 * dir;
                if (av > bv) return 1 * dir;
                return 0;
            });
            return arr;
        },

        // --- Selection ---

        fmToggleSelect(name) {
            var sel = Object.assign({}, this.fileManagerSelected);
            if (sel[name]) { delete sel[name]; } else { sel[name] = true; }
            this.fileManagerSelected = sel;
            var rows = this.fmDisplayEntries();
            this.fileManagerSelectAll = rows.length > 0 && rows.every(e => sel[e.name]);
        },

        fmToggleSelectAll() {
            this.fileManagerSelectAll = !this.fileManagerSelectAll;
            var sel = {};
            if (this.fileManagerSelectAll) {
                this.fmDisplayEntries().forEach(e => { sel[e.name] = true; });
            }
            this.fileManagerSelected = sel;
        },

        fmSelectedNames() { return Object.keys(this.fileManagerSelected); },
        fmSelectedCount() { return Object.keys(this.fileManagerSelected).length; },

        // --- Move / delete / rename / mkdir ---

        async fmMove(names, destDir) {
            if (!destDir || !names.length) return;
            this.fileManagerError = '';
            this.fileManagerErrors = {};
            try {
                const res = await api.fileAction({ action: 'move', src_dir: this.fileManagerDir, names: names, dest_dir: destDir });
                if (res && res.errors && Object.keys(res.errors).length) this.fileManagerErrors = res.errors;
            } catch (e) {
                this.fileManagerError = 'Move failed: ' + (e.message || e);
            }
            this.fileManagerSelected = {};
            this.fileManagerSelectAll = false;
            await this.loadFileManager(this.fileManagerDir);
        },

        async fmDeleteNames(names) {
            if (!names.length) return;
            this.fileManagerError = '';
            this.fileManagerErrors = {};
            try {
                const res = await api.fileAction({ action: 'delete', dir: this.fileManagerDir, names: names });
                if (res && res.errors && Object.keys(res.errors).length) this.fileManagerErrors = res.errors;
            } catch (e) {
                this.fileManagerError = 'Delete failed: ' + (e.message || e);
            }
            this.fileManagerSelected = {};
            this.fileManagerSelectAll = false;
            await this.loadFileManager(this.fileManagerDir);
        },

        // Shared two-click "Confirm?" pattern (see _dlConfirm) keyed by 'selected' or 'row:<name>'.
        async fmConfirmDelete(key, doDelete) {
            if (this._fmConfirm !== key) {
                this._fmConfirm = key;
                clearTimeout(this._fmConfirmTimer);
                this._fmConfirmTimer = setTimeout(() => { this._fmConfirm = null; }, 3000);
                return;
            }
            this._fmConfirm = null;
            await doDelete();
        },

        fmStartRename(entry) {
            this.fileManagerRename = { name: entry.name, value: entry.name };
        },

        fmCancelRename() {
            this.fileManagerRename = { name: null, value: '' };
        },

        async fmSaveRename(entry) {
            var newName = (this.fileManagerRename.value || '').trim();
            this.fileManagerRename = { name: null, value: '' };
            if (!newName || newName === entry.name) return;
            this.fileManagerError = '';
            try {
                await api.fileAction({ action: 'rename', dir: this.fileManagerDir, name: entry.name, new_name: newName });
                await this.loadFileManager(this.fileManagerDir);
            } catch (e) {
                this.fileManagerError = 'Rename failed: ' + (e.message || e);
            }
        },

        fmStartNewFolder() {
            this.fileManagerNewFolderOpen = true;
            this.fileManagerNewFolderName = '';
        },

        fmCancelNewFolder() {
            this.fileManagerNewFolderOpen = false;
            this.fileManagerNewFolderName = '';
        },

        async fmCreateFolder() {
            var name = (this.fileManagerNewFolderName || '').trim();
            if (!name) { this.fmCancelNewFolder(); return; }
            this.fileManagerError = '';
            try {
                await api.fileAction({ action: 'mkdir', dir: this.fileManagerDir, name: name });
                this.fmCancelNewFolder();
                await this.loadFileManager(this.fileManagerDir);
            } catch (e) {
                this.fileManagerError = 'Create folder failed: ' + (e.message || e);
            }
        },

        // --- Drag & drop ---

        fmDragStart(ev, entry) {
            var names = this.fileManagerSelected[entry.name] ? this.fmSelectedNames() : [entry.name];
            this._fmDragNames = names;
            ev.dataTransfer.effectAllowed = 'move';
            ev.dataTransfer.setData('text/plain', names.join(','));
        },

        fmDragOverTarget(ev, path) {
            if (!this._fmDragNames || !this._fmDragNames.length) return;
            ev.preventDefault();
            this.fileManagerDragOverPath = path;
        },

        fmDragLeaveTarget(path) {
            if (this.fileManagerDragOverPath === path) this.fileManagerDragOverPath = null;
        },

        async fmDropTarget(ev, path) {
            ev.preventDefault();
            this.fileManagerDragOverPath = null;
            var names = this._fmDragNames || [];
            this._fmDragNames = null;
            if (!names.length || !path) return;
            if (path === this.fileManagerDir) return; // dropping into the folder it's already in
            var base = path.split('/').pop();
            if (names.indexOf(base) !== -1 && path === this.fmJoin(this.fileManagerDir, base)) return; // folder onto itself
            await this.fmMove(names, path);
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
            api.getIndexStatsDetail()
                .then(d => { this.indexDetail = d; })
                .catch(e => { console.error('loadIndexStatsDetail', e); });
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
                : { id: null, server_id: this.settingsServerId, name: '', search_command: '', download_channel: '', search_bot: '', search_timeout: 10, auto_join: true, enabled: true };
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
                : { id: null, server_id: this.settingsServerId, name: '', display_name: '', search_command: '', download_channel: '', search_bot: '', search_timeout: 10, auto_join: true, enabled: true };
            this.showRealmForm = true;
            if (!this.patternSettingsList.length) {
                api.getParsePatterns().then(r => { this.patternSettingsList = Array.isArray(r) ? r : []; }).catch(() => {});
            }
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

        // --- Library (auto-organize) settings ---

        async loadLibrarySettings() {
            try {
                const [cfg, kinds] = await Promise.all([api.getLibrary(), api.getLibraryKinds()]);
                this.library = cfg || { auto_organize: false, search_depth: 3, media_root: '', categories: [] };
                this.libraryKinds = kinds || {};
                this._libraryOriginal = JSON.stringify(this.library);
                this.libraryError = '';
                this.libraryNote = '';
            } catch (e) {
                console.error('loadLibrarySettings error', e);
            }
        },

        async saveLibrarySettings() {
            this.libraryError = '';
            try {
                const saved = await api.saveLibrary(this.library);
                this.library = saved;
                this._libraryOriginal = JSON.stringify(this.library);
                this.libraryNote = '';
                this.librarySaved = true;
                setTimeout(() => { this.librarySaved = false; }, 2000);
            } catch (e) {
                this.libraryError = e.message || 'Save failed';
            }
        },

        async detectLibrarySettings() {
            try {
                this.library = await api.detectLibrary();
                this.libraryNote = 'Proposal loaded — review and Save';
                this.libraryError = '';
            } catch (e) {
                console.error('detectLibrary error', e);
            }
        },

        reloadLibrarySettings() {
            if (this._libraryOriginal) this.library = JSON.parse(this._libraryOriginal);
            this.libraryNote = '';
            this.libraryError = '';
        },

        libraryFieldsFor(kind) {
            return (this.libraryKinds && this.libraryKinds[kind]) || [];
        },

        libraryExtensionsText(cat) { return (cat.extensions || []).join(', '); },
        setLibraryExtensions(cat, text) {
            cat.extensions = text.split(',').map(s => s.trim()).filter(Boolean);
        },
        libraryPatternsText(cat) { return (cat.patterns || []).join('\n'); },
        setLibraryPatterns(cat, text) {
            cat.patterns = text.split('\n').map(s => s.trim()).filter(Boolean);
        },

        previewLibraryName() {
            clearTimeout(this._libraryPreviewTimer);
            const name = (this.libraryPreviewName || '').trim();
            if (!name) { this.libraryPreviewResult = null; return; }
            this._libraryPreviewTimer = setTimeout(async () => {
                try {
                    const res = await api.previewLibrary([name]);
                    this.libraryPreviewResult = Array.isArray(res) ? res[0] : null;
                } catch (e) {
                    console.error('previewLibrary error', e);
                }
            }, 300);
        },

        // --- Pattern settings ---

        async loadPatternSettings() {
            try {
                const [patterns, servers] = await Promise.all([api.getParsePatterns(), api.getServers()]);
                this.patternSettingsList = Array.isArray(patterns) ? patterns : [];
                const opts = [];
                for (const srv of (Array.isArray(servers) ? servers : [])) {
                    const realms = await api.getRealms(srv.id).catch(() => []);
                    for (const r of (Array.isArray(realms) ? realms : [])) {
                        const label = (srv.name || srv.host) + ' / ' + r.name;
                        opts.push({ label, server_id: srv.id, channel: r.name });
                    }
                }
                this.patternScopeOptions = opts;
            } catch (e) {
                console.error('loadPatternSettings error', e);
            }
        },

        async setPatternScope(pattern, serverID, channel) {
            const updated = Object.assign({}, pattern, {
                server_id: serverID || null,
                channel: channel || '',
            });
            try {
                await api.updateParsePattern(pattern.id, updated);
                const idx = this.patternSettingsList.findIndex(p => p.id === pattern.id);
                if (idx !== -1) {
                    this.patternSettingsList[idx] = Object.assign({}, this.patternSettingsList[idx], { server_id: serverID || null, channel: channel || '' });
                    this.patternSettingsList = this.patternSettingsList.slice();
                }
            } catch (e) {
                console.error('setPatternScope error', e);
            }
        },

        parseTags(tagsStr) {
            if (!tagsStr) return [];
            try {
                const arr = JSON.parse(tagsStr);
                return Array.isArray(arr) ? arr : [];
            } catch (e) {
                return [];
            }
        },

        openTagEdit(pattern) {
            const tags = this.parseTags(pattern.tags);
            this.patternTagEdit = { id: pattern.id, value: tags.join(', ') };
        },

        async removeTag(pattern, tag) {
            const tags = this.parseTags(pattern.tags).filter(t => t !== tag);
            await this._savePatternTagsArr(pattern, tags);
        },

        async savePatternTags(pattern) {
            if (this.patternTagEdit.id !== pattern.id) return;
            const raw = this.patternTagEdit.value;
            const tags = raw.split(',').map(t => t.trim()).filter(t => t.length > 0);
            this.patternTagEdit = { id: null, value: '' };
            await this._savePatternTagsArr(pattern, tags);
        },

        async _savePatternTagsArr(pattern, tags) {
            try {
                const updated = Object.assign({}, pattern, { tags: JSON.stringify(tags) });
                await api.updateParsePattern(pattern.id, updated);
                // Update local list
                const idx = this.patternSettingsList.findIndex(p => p.id === pattern.id);
                if (idx !== -1) {
                    this.patternSettingsList[idx] = Object.assign({}, this.patternSettingsList[idx], { tags: JSON.stringify(tags) });
                    this.patternSettingsList = this.patternSettingsList.slice();
                }
            } catch (e) {
                console.error('savePatternTags error', e);
            }
        },

        _patternsToYaml(patterns) {
            const lines = ['# xirc pattern export - v1', 'patterns:'];
            for (const p of patterns) {
                lines.push('  - name: ' + JSON.stringify(p.name));
                lines.push('    regex: \'' + (p.regex || '').replace(/'/g, "''") + '\'');
                // field_mapping
                let fm = p.field_mapping;
                if (typeof fm === 'string') {
                    try { fm = JSON.parse(fm); } catch (e) { fm = {}; }
                }
                if (fm && typeof fm === 'object' && Object.keys(fm).length > 0) {
                    lines.push('    field_mapping:');
                    for (const [k, v] of Object.entries(fm)) {
                        lines.push('      ' + k + ': ' + v);
                    }
                } else {
                    lines.push('    field_mapping: {}');
                }
                lines.push('    priority: ' + (p.priority || 0));
                const tags = this.parseTags(p.tags);
                if (tags.length === 0) {
                    lines.push('    tags: []');
                } else {
                    lines.push('    tags:');
                    for (const t of tags) {
                        lines.push('      - ' + JSON.stringify(t));
                    }
                }
            }
            return lines.join('\n') + '\n';
        },

        _downloadText(filename, content, mimeType) {
            const blob = new Blob([content], { type: mimeType });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        },

        _exportDateStr() {
            const d = new Date();
            const pad = n => String(n).padStart(2, '0');
            return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
        },

        exportPatternsYAML() {
            if (!this.patternSettingsList.length) {
                alert('No patterns to export. Open the Patterns tab first.');
                return;
            }
            const yaml = this._patternsToYaml(this.patternSettingsList);
            this._downloadText('mxirc-patterns-' + this._exportDateStr() + '.yaml', yaml, 'text/yaml');
        },

        exportPatternsJSON() {
            if (!this.patternSettingsList.length) {
                alert('No patterns to export. Open the Patterns tab first.');
                return;
            }
            this.exportPatternsDialog.json = this._buildPatternsExportJSON();
            this.exportPatternsDialog.copied = false;
            this.exportPatternsDialog.open = true;
        },

        _buildPatternsExportJSON() {
            const data = this.patternSettingsList.map(p => {
                let fm = p.field_mapping;
                if (typeof fm === 'string') { try { fm = JSON.parse(fm); } catch (e) { fm = {}; } }
                return {
                    name: p.name,
                    regex: p.regex,
                    field_mapping: fm || {},
                    priority: p.priority || 0,
                    tags: this.parseTags(p.tags),
                };
            });
            return JSON.stringify({ patterns: data }, null, 2);
        },

        closeExportPatternsDialog() {
            this.exportPatternsDialog.open = false;
        },

        downloadExportedPatternsJSON() {
            this._downloadText('mxirc-patterns-' + this._exportDateStr() + '.json', this.exportPatternsDialog.json, 'application/json');
        },

        async copyExportedPatternsJSON() {
            try {
                await navigator.clipboard.writeText(this.exportPatternsDialog.json);
                this.exportPatternsDialog.copied = true;
                setTimeout(() => { this.exportPatternsDialog.copied = false; }, 2000);
            } catch (e) {
                console.error('copyExportedPatternsJSON failed', e);
            }
        },

        async importPatternsFile(event) {
            this.patternImportMsg = '';
            this.patternImportError = false;
            const file = event.target.files && event.target.files[0];
            if (!file) return;
            // Reset input so same file can be re-selected
            event.target.value = '';
            const text = await file.text();
            let patterns = [];
            const lower = file.name.toLowerCase();
            try {
                if (lower.endsWith('.json')) {
                    const parsed = JSON.parse(text);
                    patterns = Array.isArray(parsed) ? parsed : (Array.isArray(parsed.patterns) ? parsed.patterns : []);
                } else {
                    // Minimal YAML parser for the specific export format
                    patterns = this._parseExportYaml(text);
                }
            } catch (e) {
                this.patternImportMsg = 'Parse error: ' + e.message;
                this.patternImportError = true;
                return;
            }
            if (!patterns.length) {
                this.patternImportMsg = 'No patterns found in file.';
                this.patternImportError = true;
                return;
            }
            try {
                const res = await api.importParsePatterns(patterns);
                this.patternImportMsg = 'Imported ' + (res.imported || 0) + ', skipped ' + (res.skipped || 0) + '.';
                this.patternImportError = false;
                await this.loadPatternSettings();
            } catch (e) {
                this.patternImportMsg = 'Import failed: ' + e.message;
                this.patternImportError = true;
            }
            // Auto-clear message after 5 seconds
            setTimeout(() => { this.patternImportMsg = ''; }, 5000);
        },

        // Minimal line-based YAML parser — handles only the mxirc export format.
        _parseExportYaml(text) {
            const lines = text.split('\n');
            const patterns = [];
            let cur = null;
            let inFieldMapping = false;
            let inTags = false;

            for (let i = 0; i < lines.length; i++) {
                const raw = lines[i];
                const stripped = raw.trimEnd();

                // Skip comment and top-level "patterns:" key
                if (stripped.startsWith('#') || stripped.trim() === 'patterns:') continue;

                // New pattern entry: "  - name: ..."
                const newEntry = stripped.match(/^\s+-\s+name:\s*(.*)/);
                if (newEntry) {
                    if (cur) patterns.push(cur);
                    cur = { name: _yamlUnquote(newEntry[1].trim()), regex: '', field_mapping: {}, priority: 0, tags: [] };
                    inFieldMapping = false;
                    inTags = false;
                    continue;
                }

                if (!cur) continue;

                // Detect block keys at pattern level (indent ~4)
                const keyVal = stripped.match(/^(\s+)(\w+):\s*(.*)/);
                if (!keyVal) continue;
                const keyIndent = keyVal[1].length;
                const key = keyVal[2];
                const val = keyVal[3].trim();

                if (keyIndent <= 4 && key === 'field_mapping') {
                    inFieldMapping = true;
                    inTags = false;
                    if (val && val !== '{}') {
                        // inline: unlikely in our format but handle anyway
                    }
                    continue;
                }
                if (keyIndent <= 4 && key === 'tags') {
                    inFieldMapping = false;
                    inTags = true;
                    if (val === '[]') { cur.tags = []; inTags = false; }
                    continue;
                }
                if (keyIndent <= 4 && key === 'regex') {
                    inFieldMapping = false;
                    inTags = false;
                    cur.regex = _yamlUnquote(val);
                    continue;
                }
                if (keyIndent <= 4 && key === 'priority') {
                    inFieldMapping = false;
                    inTags = false;
                    cur.priority = parseInt(val, 10) || 0;
                    continue;
                }

                // field_mapping sub-key (indent 6+)
                if (inFieldMapping && keyIndent >= 6) {
                    cur.field_mapping[key] = parseInt(val, 10) || val;
                    continue;
                }

                // tag list item: "      - value"
                if (inTags) {
                    const tagItem = stripped.match(/^\s+-\s+(.*)/);
                    if (tagItem) {
                        cur.tags.push(_yamlUnquote(tagItem[1].trim()));
                    }
                    continue;
                }

                // If we hit any other key at pattern indent, reset sub-mode
                if (keyIndent <= 4) {
                    inFieldMapping = false;
                    inTags = false;
                }
            }
            if (cur) patterns.push(cur);
            return patterns;

            function _yamlUnquote(s) {
                if (!s) return '';
                if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
                    const inner = s.slice(1, -1);
                    // Unescape single-quote doubling ('') for single-quoted strings
                    if (s.startsWith("'")) return inner.replace(/''/g, "'");
                    return inner.replace(/\\"/g, '"').replace(/\\\\/g, '\\');
                }
                return s;
            }
        },

        testPattern() {
            this.patternTesterResult = null;
            const line = this.patternTesterLine.trim();
            if (!line) return;
            const sorted = this.patternSettingsList.slice().sort((a, b) => (b.priority || 0) - (a.priority || 0));
            for (const p of sorted) {
                if (!p.enabled) continue;
                let re;
                try { re = new RegExp(p.regex); } catch (e) { continue; }
                const m = re.exec(line);
                if (!m) continue;
                // Parse field mapping
                let fm = p.field_mapping;
                if (typeof fm === 'string') { try { fm = JSON.parse(fm); } catch (e) { fm = {}; } }
                const knownFields = ['pack_number', 'filename', 'filesize', 'downloads_count', 'bot_nick'];
                const fields = [];
                for (const field of knownFields) {
                    if (fm && fm[field] != null) {
                        const idx = parseInt(fm[field], 10);
                        if (!isNaN(idx)) {
                            fields.push({ name: field, value: m[idx] != null ? m[idx] : '' });
                        }
                    }
                }
                this.patternTesterResult = { matched: true, name: p.name, priority: p.priority, fields };
                return;
            }
            this.patternTesterResult = { matched: false };
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
            this.loadIndexStats();
            var self = this;
            setInterval(function() { self.loadDiskStats(); }, 30000);

            // Restore layout prefs from localStorage
            this._channelLayout = {};
            for (let i = 0; i < localStorage.length; i++) {
                const k = localStorage.key(i);
                if (k && k.startsWith('mxirc_layout_')) {
                    this._channelLayout[k.slice('mxirc_layout_'.length)] = localStorage.getItem(k);
                }
            }
            this.statsOnlyDefault = localStorage.getItem('mxirc_stats_only') === 'true';

            // Load servers
            try {
                const res = await api.getServers();
                this.servers = Array.isArray(res) ? res : [];
                for (const srv of this.servers) {
                    this.expandedServers[srv.id] = true;
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
