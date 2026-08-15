package nat

import "fmt"

// AllocatePort reserves a host port within [start, end]. When want > 0 it
// attempts to reserve exactly that port instead. A protocol is respected so
// tcp and udp can share a port number.
func (m *Manager) AllocatePort(want, start, end int, protocol string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if want > 0 {
		if want < 1 || want > 65535 {
			return 0, fmt.Errorf("端口 %d 无效", want)
		}
		if m.portTaken(want, protocol) {
			return 0, fmt.Errorf("端口 %d 已被占用", want)
		}
		return want, nil
	}
	if start <= 0 || start > end {
		return 0, fmt.Errorf("端口范围无效")
	}
	for port := start; port <= end; port++ {
		if !m.portTaken(port, protocol) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("端口池 %d-%d 已满", start, end)
}

func (m *Manager) portTaken(port int, protocol string) bool {
	for _, r := range m.rules {
		if r.HostPort == port && (protocol == "" || r.Protocol == protocol) {
			return true
		}
	}
	return false
}
