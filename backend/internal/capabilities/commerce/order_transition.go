package commerce

func OrderTransitionAllowed(current, target string, force bool) bool {
	if current == target {
		return true
	}
	switch target {
	case "paid":
		return current == "pending" || current == "failed" || (force && current == "canceled")
	case "failed":
		return current == "pending"
	case "canceled":
		return current == "pending" || (force && current == "failed")
	default:
		return false
	}
}
