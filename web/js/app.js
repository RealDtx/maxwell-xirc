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

        // --- Computed helpers ---

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
            this.runSearch();
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
                const status = data.data;  // "connected" / "disconnected" / "connecting"
                this.ircStatus[data.server_id] = Object.assign({}, this.ircStatus[data.server_id] || {}, { status: status });
            }
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

        // --- Init ---

        async init() {
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
                    statusMap[s.server_id] = { status: s.status, channels: s.channels, name: s.server_name };
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

            // Load downloads
            await this.loadDownloads();

            // Load saved searches
            await this.loadSavedSearches();

            // Start WebSocket
            this.wsConnect();
        },
    }));
});
