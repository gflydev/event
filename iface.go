package event

// IEvent is the interface that all events must implement.
// Events are simple data containers holding information about something that occurred.
//
// Convention for EventName: "domain.action" (e.g., "user.registered", "order.shipped")
type IEvent interface {
	// EventName returns the unique identifier for this event type.
	EventName() string
}

// IListener with T type is the generic interface for typed event listeners.
// T is the concrete event type the listener handles directly — no type assertion needed.
type IListener[T IEvent] interface {
	// Handle processes the concrete event.
	// Return a non-nil error to halt further listener execution for this event.
	Handle(event T) error
}

// ISubscriber groups multiple listeners for one or more events.
// Useful for organising all listeners related to a single domain.
type ISubscriber interface {
	// Subscribe registers the subscriber's listeners on the given dispatcher.
	Subscribe(dispatcher *Dispatcher)
}
