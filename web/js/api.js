const _apiBase = (window.XIRC_PREFIX || '') + '/api';

const api = {
    async request(method, path, body) {
        const opts = { method, headers: {} };
        if (body !== undefined) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        } else if (method === 'POST' || method === 'PUT') {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = '{}';
        }
        const res = await fetch(_apiBase + path, opts);
        if (res.status === 401 && !path.startsWith('/auth/')) {
            window.dispatchEvent(new CustomEvent('xirc:unauthorized'));
        }
        if (!res.ok && method !== 'GET') {
            const err = await res.json().catch(() => ({ error: res.statusText }));
            throw new Error(err.error || res.statusText);
        }
        return res.json();
    },
    get(path)        { return this.request('GET', path); },
    post(path, body) { return this.request('POST', path, body); },
    put(path, body)  { return this.request('PUT', path, body); },
    del(path)        { return this.request('DELETE', path); },

    // Auth & users
    me()                  { return this.get('/auth/me'); },
    login(username, password) { return this.post('/auth/login', { username, password }); },
    setupAdmin(username, password) { return this.post('/auth/setup', { username, password }); },
    logout()              { return this.post('/auth/logout', {}); },
    getUsers()            { return this.get('/users'); },
    createUser(u)         { return this.post('/users', u); },
    updateUser(id, patch) { return this.put('/users/' + id, patch); },
    deleteUser(id)        { return this.del('/users/' + id); },

    // Servers
    getServers()           { return this.get('/servers'); },
    createServer(s)        { return this.post('/servers', s); },
    updateServer(id, s)    { return this.put('/servers/' + id, s); },
    deleteServer(id)       { return this.del('/servers/' + id); },
    getRealms(serverId)    { return this.get(`/realms?server_id=${serverId}`); },
    createRealm(r)         { return this.post('/realms', r); },
    updateRealm(id, r)     { return this.put('/realms/' + id, r); },
    deleteRealm(id)        { return this.del('/realms/' + id); },

    // IRC
    getIRCStatus()                    { return this.get('/irc/status'); },
    connectServer(serverId)           { return this.post('/irc/connect', { server_id: serverId }); },
    disconnectServer(serverId)        { return this.post('/irc/disconnect', { server_id: serverId }); },
    joinChannel(serverId, channel, key='') { return this.post('/irc/join', { server_id: serverId, channel, key }); },
    sendMessage(serverId, target, msg){ return this.post('/irc/message', { server_id: serverId, target, message: msg }); },
    sendRaw(serverId, cmd)            { return this.post('/irc/raw', { server_id: serverId, command: cmd }); },
    getIRCMessages(serverId, channel, before, limit) {
        let q = `/irc/${serverId}/messages?channel=${encodeURIComponent(channel)}`;
        if (before) q += `&before=${encodeURIComponent(before)}`;
        if (limit)  q += `&limit=${limit}`;
        return this.get(q);
    },
    getIRCNames(serverId, channel) {
        return this.get(`/irc/${serverId}/names?channel=${encodeURIComponent(channel)}`);
    },

    // Search
    startSearch(serverId, channel, query) {
        return this.post('/search/start', { server_id: serverId, channel, query });
    },
    stopSearch(serverId, channel) {
        return this.post('/search/stop', { server_id: serverId, channel });
    },
    getSearchResults(query, serverId, channel) {
        return this.get(`/search/results?query=${encodeURIComponent(query)}&server_id=${serverId}&channel=${encodeURIComponent(channel)}`);
    },
    getAllSearchResults(query, since) {
        let url = `/search/results?query=${encodeURIComponent(query)}`;
        if (since != null) url += `&since=${since}`;
        return this.get(url);
    },
    getUnmatchedSamples(serverId, query, since, limit) {
        let url = `/search/unmatched?server_id=${serverId}&query=${encodeURIComponent(query)}`;
        if (since != null) url += `&since=${since}`;
        if (limit  != null) url += `&limit=${limit}`;
        return this.get(url);
    },
    learnPattern(body) { return this.post('/search/patterns/learn', body); },
    getSavedSearches()        { return this.get('/search/saved'); },
    createSavedSearch(s)      { return this.post('/search/saved', s); },
    deleteSavedSearch(id)     { return this.del('/search/saved/' + id); },
    getParsePatterns()           { return this.get('/search/patterns'); },
    createParsePattern(p)        { return this.post('/search/patterns', p); },
    updateParsePattern(id, p)    { return this.put('/search/patterns/' + id, p); },
    importParsePatterns(patterns){ return this.post('/search/patterns/import', { patterns }); },

    // Self-collected search index — instant offline search, no IRC round-trip.
    searchIndex(query, serverId, channel) {
        let url = `/index/search?query=${encodeURIComponent(query)}`;
        if (serverId) url += `&server_id=${serverId}`;
        if (channel)  url += `&channel=${encodeURIComponent(channel)}`;
        return this.get(url);
    },
    getIndexStats(serverId) {
        return this.get('/index/stats' + (serverId ? '?server_id=' + serverId : ''));
    },
    clearIndex(serverId) {
        return this.post('/index/clear', { server_id: serverId || 0 });
    },
    getIndexStatsDetail() {
        return this.get('/index/stats/detail');
    },

    // Downloads
    getDownloads(status)  { return this.get('/downloads' + (status ? '?status=' + status : '')); },
    requestDownload(req)  { return this.post('/downloads/request', req); },
    cancelDownload(id)    { return this.post('/downloads/cancel', { download_id: id }); },
    retryDownload(id)     { return this.post('/downloads/retry', { download_id: id }); },
    moveDownload(id)      { return this.post('/downloads/move', { download_id: id }); },
    deleteDownloads(ids) { return this.post('/downloads/delete', { ids }); },
    clearDownloads(status) { return this.post('/downloads/clear', { status }); },
    setAutoExtract(id, enabled) { return this.post('/downloads/set-auto-extract', { download_id: id, auto_extract: enabled }); },
    getDownloadTargets() { return this.get('/downloads/targets'); },
    setDownloadTarget(id, dir) { return this.post('/downloads/set-target', { download_id: id, target_dir: dir }); },

    // Storage
    getStorageStats() { return this.get('/storage'); },

    // Errors
    getErrors(limit) {
        return this.get('/errors' + (limit ? '?limit=' + limit : ''));
    },

    // Browse & Files
    browseDir(path)  { return this.get('/browse?path=' + encodeURIComponent(path || '/')); },
    fileRoots()      { return this.get('/files'); },
    async listFiles(dir) {
        const res = await fetch(_apiBase + '/files?dir=' + encodeURIComponent(dir));
        if (res.status === 401) window.dispatchEvent(new CustomEvent('xirc:unauthorized'));
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data.error || res.statusText);
        return data;
    },
    fileAction(body) { return this.post('/files', body); },
    rawFileUrl(path, download) {
        return _apiBase + '/files/raw?path=' + encodeURIComponent(path) + (download ? '&download=1' : '');
    },

    // Stats
    getDownloadStats()                { return this.get('/stats/downloads'); },
    getDownloadHistory(offset, limit) {
        return this.get(`/stats/history?offset=${offset||0}&limit=${limit||50}`);
    },

    // Library (auto-organize categories)
    getLibrary()             { return this.get('/library'); },
    saveLibrary(cfg)         { return this.put('/library', cfg); },
    detectLibrary()          { return this.get('/library/detect'); },
    getLibraryKinds()        { return this.get('/library/kinds'); },
    previewLibrary(filenames){ return this.post('/library/preview', { filenames }); },

    // Setup wizard
    getSetupStatus() { return this.get('/setup/status'); },
    getSetupDefaults() { return this.get('/setup/defaults'); },
    completeSetup(mappings) { return this.post('/setup/complete', { mappings }); },

    // Capabilities
    getCapabilities()     { return this.get('/capabilities'); },
    recheckCapabilities() { return this.post('/capabilities/recheck', {}); },

    // Settings
    getSettings()      { return this.get('/settings'); },
    saveSettings(s)    { return this.put('/settings', s); },
};
