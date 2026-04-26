Next features / fixes:

* Consider fixing missing static file serve -> defaulting to <domain>/css etc.
* Statistics should show average download rate etc - a detailled evaluation of bots, realms regarding average, min, max download speed
* Server/Realms still not showing up right after config, needed to do Ctrl+Shift+R
* Easy server/realm config export
* All server search windows share text input: Type in one, shows up directly (without search) in the others
* Settings -> Realms should show all Realms grouped by server until server filter selected
* maXwell IRC logo - also make it customizable / configurable 
* explain pattern matching somewhere; Guess: it tests pattern with a plausibility verification by Priority; 
**! We should keep it auto-detect but configurable per-realm!!! so each realm config should come with it's own regex which might even make sense to version or at least date 
--> Test all pattern, if multiple match let user decide; 
--> Must be able to define regex on the spot, not via complicated import; best: let user select parts and generate regex from it (if possible)
- also: priority must be changable;
---> looks like currently scope is not applied at all to incoming messages from the corresponding realm's bot.
* Auto-detect chat-channel (usually only download-channel is given, chat channel is often referenced in entry message
* Own database from posts in download channels - filterable 
* Reconnect button on server, rejoin button on channel 
* Search result overview on top should show realms, not servers
* Prepare for publishing on github
* Server/Realm should have nice little icons...
