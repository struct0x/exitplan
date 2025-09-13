package exitplan

type phase int32

const (
	phaseStarting phase = iota
	phaseRunning
	phaseTeardown
)
