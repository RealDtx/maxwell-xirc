package exitcodes

// ExitTransient is used for errors that are likely to resolve on retry
// (e.g., NFS mount not yet available). systemd will restart the process.
const ExitTransient = 1

// ExitConfig is used for operator configuration errors that will not
// resolve without manual intervention (e.g., wrong directory ownership).
// Paired with RestartPreventExitStatus=78 in the systemd unit.
const ExitConfig = 78
