package irc

// IRCClient is a thin abstraction over an IRC connection.
// Implementing this interface allows swapping IRC backends
// without touching higher-level code.
type IRCClient interface {
	// Connect connects to the IRC server. Blocks until disconnected.
	// Call in a goroutine. Returns when connection ends.
	Connect() error

	// Close disconnects from the server.
	Close()

	// Join joins a channel (optionally with key).
	Join(channel, key string)

	// Part leaves a channel.
	Part(channel string)

	// Privmsg sends a message to a channel or user.
	Privmsg(target, message string)

	// Nick returns the current nickname.
	Nick() string

	// OnMessage registers a handler called for every PRIVMSG.
	// The handler receives: nick (sender), target (channel/#), message.
	OnMessage(func(nick, target, message string))

	// OnNotice registers a handler for NOTICE messages.
	OnNotice(func(nick, target, message string))

	// OnConnect registers a handler called when successfully connected and registered.
	OnConnect(func())

	// OnDisconnect registers a handler called when connection drops.
	OnDisconnect(func())

	// OnRaw registers a handler for raw IRC lines (for advanced parsing).
	OnRaw(func(line string))

	// SendLine writes a raw IRC protocol line (without CRLF — the implementation adds it).
	SendLine(line string)
}
