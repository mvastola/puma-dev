package dev

type OnAddEvent struct {
	handlers []interface{ HandleEvent(string) }
}

type OnAddEventHandler interface{ HandleEvent(string) }

var EventListener OnAddEvent

func (listener *OnAddEvent) Register(handler OnAddEventHandler) {
	listener.handlers = append(listener.handlers, handler)
}

func (listener *OnAddEvent) Trigger(payload string) {
	for _, handler := range listener.handlers {
		go handler.HandleEvent(payload)
	}
}
