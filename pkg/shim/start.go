package shim

// startContainer starts a container or sandbox within the shim service.
//
// For sandboxes (CanBeSandbox): calls sandbox.Start() and launches a watchSandbox goroutine
// to monitor the entire sandbox lifecycle.
//
// For pod containers: calls sandbox.StartContainer() to start a specific container.
//
// Sets up IO streams via sandbox.IOStream() and manages tty/non-tty IO copying.
// For containers with terminal=false and no IO fifos (like pause/infra containers),
// signals exit immediately since they don't need lifecycle monitoring.
//
// Launches waitContainerExit goroutine to monitor container termination
// and handle cleanup. On any error, sends exit code 255 to exitCh for cleanup.
func startContainer() error {}
