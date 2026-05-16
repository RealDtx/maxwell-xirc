const _apiBase = (window.XIRC_PREFIX || '') + '/api';

const api = {
    async get(path) {
        const res = await fetch(_apiBase + path);
        return res.json();
    },

    async post(path, body) {
        const res = await fetch(_apiBase + path, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({ error: res.statusText }));
            throw new Error(err.error || res.statusText);
        }
        return res.json();
    },

    async put(path, body) {
        const res = await fetch(_apiBase + path, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        return res.json();
    },

    async del(path) {
        const res = await fetch(_apiBase + path, { method: 'DELETE' });
        return res.json();
    },

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

    // Downloads
    getDownloads(status)  { return this.get('/downloads' + (status ? '?status=' + status : '')); },
    requestDownload(req)  { return this.post('/downloads/request', req); },
    cancelDownload(id)    { return this.post('/downloads/cancel', { download_id: id }); },
    retryDownload(id)     { return this.post('/downloads/retry', { download_id: id }); },
    moveDownload(id)      { return this.post('/downloads/move', { download_id: id }); },
    deleteDownloads(ids) { return this.post('/downloads/delete', { ids }); },
    clearDownloads(status) { return this.post('/downloads/clear', { status }); },
    setAutoExtract(id, enabled) { return this.post('/downloads/set-auto-extract', { download_id: id, auto_extract: enabled }); },

    // Storage
    getStorageStats() { return this.get('/storage'); },

    // Errors
    getErrors(limit) {
        return this.get('/errors' + (limit ? '?limit=' + limit : ''));
    },

    // Routing & Hooks
    getRoutingRules()            { return this.get('/routing/rules'); },
    createRoutingRule(r)         { return this.post('/routing/rules', r); },
    updateRoutingRule(id, r)     { return this.put('/routing/rules/' + id, r); },
    deleteRoutingRule(id)        { return this.del('/routing/rules/' + id); },
    getHooks()                   { return this.get('/hooks'); },
    createHook(h)                { return this.post('/hooks', h); },
    updateHook(id, h)            { return this.put('/hooks/' + id, h); },
    deleteHook(id)               { return this.del('/hooks/' + id); },

    // Browse & Files
    browseDir(path)  { return this.get('/browse?path=' + encodeURIComponent(path || '/')); },
    listFiles(dir)   { return this.get('/files?dir=' + encodeURIComponent(dir)); },

    // Stats
    getDownloadStats()                { return this.get('/stats/downloads'); },
    getDownloadHistory(offset, limit) {
        return this.get(`/stats/history?offset=${offset||0}&limit=${limit||50}`);
    },

    // Setup wizard
    getSetupStatus() { return this.get('/setup/status'); },
    getSetupDefaults() { return this.get('/setup/defaults'); },
    completeSetup(mappings) { return this.post('/setup/complete', { mappings }); },
};
