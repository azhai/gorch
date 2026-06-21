package ipc

type CommandHandler interface {
	HandleCommand(cmd ControlCommand) ControlResponse
}
