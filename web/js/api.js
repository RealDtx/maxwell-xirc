const api = {
    async get(path) {
        const res = await fetch('/api' + path);
        return res.json();
    },

    async post(path, body) {
        const res = await fetch('/api' + path, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        return res.json();
    },

    async put(path, body) {
        const res = await fetch('/api' + path, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        return res.json();
    },

    async del(path) {
        const res = await fetch('/api' + path, { method: 'DELETE' });
        return res.json();
    },

    // Servers
    getServers()           { return this.get('/servers'); },
    createServer(s)        { return this.post('/servers', s); },
    updateServer(id, s)    { return this.put('/servers/' + id, s); },
    deleteServer(id)       { return this.del('/servers/' + id); },
    getChannels(serverId)  { return this.get('/servers/' + serverId + '/channels'); },
    createChannel(ch)      { return this.post('/channels', ch); },
    updateChannel(id, ch)  { return this.put('/channels/' + id, ch); },
    deleteChannel(id)      { return this.del('/channels/' + id); },

    // IRC
    getIRCStatus()                    { return this.get('/irc/status'); },
    connectServer(serverId)           { return this.post('/irc/connect', { server_id: serverId }); },
    disconnectServer(serverId)        { return this.post('/irc/disconnect', { server_id: serverId }); },
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
    getSearchResults(query, serverId, channel) {
        return this.get(`/search/results?query=${encodeURIComponent(query)}&server_id=${serverId}&channel=${encodeURIComponent(channel)}`);
    },
    getSavedSearches()        { return this.get('/search/saved'); },
    createSavedSearch(s)      { return this.post('/search/saved', s); },
    deleteSavedSearch(id)     { return this.del('/search/saved/' + id); },
    getParsePatterns()        { return this.get('/search/patterns'); },
    createParsePattern(p)     { return this.post('/search/patterns', p); },
    updateParsePattern(id, p) { return this.put('/search/patterns/' + id, p); },

    // Downloads
    getDownloads(status)  { return this.get('/downloads' + (status ? '?status=' + status : '')); },
    requestDownload(req)  { return this.post('/downloads/request', req); },
    cancelDownload(id)    { return this.post('/downloads/cancel', { download_id: id }); },
    retryDownload(id)     { return this.post('/downloads/retry', { download_id: id }); },
    moveDownload(id)      { return this.post('/downloads/move', { download_id: id }); },

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
};
