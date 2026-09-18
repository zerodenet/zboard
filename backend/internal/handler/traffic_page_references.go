package handler

type trafficPageReferences struct {
	Subscriptions map[string]entityReference `json:"subscriptions"`
	Nodes         map[string]entityReference `json:"nodes"`
}
