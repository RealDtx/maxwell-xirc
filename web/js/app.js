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
        downloads: [],
        ircMessages: {},
        ircInput: '',
        showIrcConsole: false,
        ws: null,
        wsRetryTimer: null,

        // --- Computed helpers ---

        serverStatus(serverId) {
            const s = this.ircStatus[serverId];
            return s ? s.status : 'disconnected';
        },

        channelKey(serverId, channel) {
            return serverId + ':' + channel;
        },

        activeMessages() {
            if (!this.activeServer || !this.activeChannel) return [];
            return this.ircMessages[this.channelKey(this.activeServer, this.activeChannel)] || [];
        },

        // --- Navigation ---

        setView(view) {
            this.activeView = view;
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

        sendMessage() {
            const msg = this.ircInput.trim();
            if (!msg || !this.activeServer || !this.activeChannel) return;
            api.sendMessage(this.activeServer, this.activeChannel, msg);
            this.ircInput = '';
        },

        // --- Search ---

        async runSearch() {
            if (!this.activeServer || !this.activeChannel || !this.searchQuery.trim()) return;
            this.searchRunning = true;
            this.searchResults = [];
            try {
                await api.startSearch(this.activeServer, this.activeChannel, this.searchQuery);
                const res = await api.getSearchResults(this.searchQuery, this.activeServer, this.activeChannel);
                this.searchResults = res.results || [];
            } catch (e) {
                console.error('search error', e);
            } finally {
                this.searchRunning = false;
            }
        },

        requestDownload(result) {
            api.requestDownload({
                server_id: this.activeServer,
                bot_nick: result.bot_nick || result.nick,
                pack_number: result.pack_number || result.pack,
            }).catch(e => console.error('download request error', e));
        },

        // --- Downloads ---

        async loadDownloads() {
            try {
                const res = await api.getDownloads();
                this.downloads = res.downloads || [];
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

        downloadProgress(dl) {
            if (!dl.bytes_total || dl.bytes_total === 0) return 0;
            return Math.round((dl.bytes_received / dl.bytes_total) * 100);
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
            const type = data.event_type;

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
            } else if (type === 'download_progress') {
                const dl = this.downloads.find(d => d.id === data.download_id);
                if (dl) {
                    dl.bytes_received = data.bytes_received;
                    dl.bytes_total = data.bytes_total;
                    dl.speed = data.speed;
                }
            } else if (type === 'download_complete') {
                this.loadDownloads();
            } else if (type === 'download_failed') {
                this.loadDownloads();
            } else if (type === 'irc_connected') {
                if (this.ircStatus[data.server_id]) {
                    this.ircStatus[data.server_id].status = 'connected';
                }
            } else if (type === 'irc_disconnected') {
                if (this.ircStatus[data.server_id]) {
                    this.ircStatus[data.server_id].status = 'disconnected';
                }
            }
        },

        // --- Init ---

        async init() {
            // Load servers
            try {
                const res = await api.getServers();
                this.servers = res.servers || [];
                if (this.servers.length > 0) {
                    this.expandedServers[this.servers[0].id] = true;
                }
            } catch (e) {
                console.error('loadServers error', e);
            }

            // Load IRC status and merge channels into server objects
            try {
                const res = await api.getIRCStatus();
                const statusMap = {};
                for (const s of (res.servers || [])) {
                    statusMap[s.server_id] = s;
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

            // Start WebSocket
            this.wsConnect();
        },
    }));
});
