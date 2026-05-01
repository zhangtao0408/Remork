//go:build windows

package apply

func processAlive(pid int) bool {
	return pid > 0
}
