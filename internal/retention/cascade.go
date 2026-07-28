package retention

func CascadeTargets(registry Registry, className string) []string {
	class, ok := registry[className]
	if !ok {
		return nil
	}
	out := make([]string, len(class.Cascade))
	copy(out, class.Cascade)
	return out
}
