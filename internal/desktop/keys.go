package desktop

import "strconv"

func ValidKey(k string) bool {
	if len(k) == 1 && ((k[0] >= 'a' && k[0] <= 'z') || (k[0] >= '0' && k[0] <= '9')) {
		return true
	}
	switch k {
	case "ctrl", "alt", "shift", "meta", "enter", "tab", "escape", "space", "backspace", "delete", "home", "end", "pageup", "pagedown", "left", "right", "up", "down":
		return true
	}
	if len(k) > 1 && k[0] == 'f' {
		n, err := strconv.Atoi(k[1:])
		return err == nil && n >= 1 && n <= 12 && k == "f"+strconv.Itoa(n)
	}
	return false
}
