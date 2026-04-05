package event

import (
	"reflect"
	"sync"

	"github.com/gflydev/core/log"
)

// handler is the internal function type stored by the dispatcher.
// The type assertion from IEvent → T is performed once at registration time (inside ListenOn),
// not on every dispatch call.
type handler func(IEvent) error

// Dispatcher manages event-handler registrations and event dispatching.
// It is safe for concurrent use.
type Dispatcher struct {
	handlers map[string][]handler
	mu       sync.RWMutex
}

// global is the default package-level dispatcher instance.
var global = &Dispatcher{
	handlers: make(map[string][]handler),
}

// ---------------------------------------------------------------
//                      Dispatcher methods
// ---------------------------------------------------------------

// listen registers an internal handler function for the given event name.
func (d *Dispatcher) listen(eventName string, h handler) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.handlers[eventName] = append(d.handlers[eventName], h)
}

// Dispatch fires an event synchronously to all registered handlers.
// Handlers are called in registration order.
// Returns on the first handler error, stopping further propagation.
func (d *Dispatcher) Dispatch(event IEvent) error {
	d.mu.RLock()
	src := d.handlers[event.EventName()]
	handlers := make([]handler, len(src))
	copy(handlers, src)
	d.mu.RUnlock()

	for _, h := range handlers {
		if err := h(event); err != nil {
			log.Errorf("[Event] handler error for '%s': %v", event.EventName(), err)
			return err
		}
	}

	return nil
}

// DispatchAsync fires an event asynchronously in a new goroutine (fire-and-forget).
// Errors are logged but not propagated to the caller.
func (d *Dispatcher) DispatchAsync(event IEvent) {
	go func() {
		if err := d.Dispatch(event); err != nil {
			log.Errorf("[Event] async dispatch error for '%s': %v", event.EventName(), err)
		}
	}()
}

// Subscribe registers all listeners defined by the given subscriber.
func (d *Dispatcher) Subscribe(subscriber ISubscriber) {
	subscriber.Subscribe(d)
}

// ---------------------------------------------------------------
//                  Generic registration helpers
// ---------------------------------------------------------------

// ListenOn registers a typed listener on a specific dispatcher instance.
// The event name is inferred automatically from T — safe for both value and pointer types.
//
// Use this inside ISubscriber.Subscribe() implementations:
//
//	func (s *OrderSubscriber) Subscribe(d *events.Dispatcher) {
//	    events.ListenOn[events.OrderPlaced](d, &SendConfirmationListener{})
//	}
func ListenOn[T IEvent](d *Dispatcher, listener IListener[T]) {
	// Derive the event name without risking a nil-pointer dereference.
	// reflect.TypeOf((*T)(nil)).Elem() gives the reflect.Type of T itself,
	// regardless of whether T is a value type or a pointer type.
	// For pointer types (Kind == Ptr) we allocate a new instance via reflect.New
	// so that EventName() can be called safely on a non-nil value.
	t := reflect.TypeOf((*T)(nil)).Elem()

	var zero T // T must be a value type; pointer events are not supported
	if t.Kind() == reflect.Ptr {
		zero = reflect.New(t.Elem()).Interface().(T) // allocate a real non-nil instance
	}

	d.listen(zero.EventName(), func(event IEvent) error {
		e, ok := event.(T)
		if !ok {
			return nil
		}
		return listener.Handle(e)
	})
}

// ---------------------------------------------------------------
//                  Global convenience functions
// ---------------------------------------------------------------

// Listen registers a typed listener on the global dispatcher.
// The event name is inferred automatically from T.
//
//	events.Listen[events.UserRegistered](&SendWelcomeEmailListener{})
func Listen[T IEvent](listener IListener[T]) {
	ListenOn[T](global, listener)
}

// Dispatch fires an event synchronously on the global dispatcher.
//
// Parameters:
//   - event (IEvent): The event to dispatch.
//
// Returns:
//   - error: The first error returned by a handler, or nil.
func Dispatch(event IEvent) error {
	return global.Dispatch(event)
}

// DispatchAsync fires an event asynchronously on the global dispatcher.
// The event is handled in a background goroutine; errors are only logged.
//
// Parameters:
//   - event (IEvent): The event to dispatch.
func DispatchAsync(event IEvent) {
	global.DispatchAsync(event)
}

// Subscribe registers a subscriber on the global dispatcher.
//
// Parameters:
//   - subscriber (ISubscriber): The subscriber to register.
func Subscribe(subscriber ISubscriber) {
	global.Subscribe(subscriber)
}
