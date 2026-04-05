# gFly Event

This guide covers the Event-Listener system in gFly — an implementation of the Observer Pattern that decouples business logic by allowing events to be dispatched and handled by independent listeners

    Copyright © 2023, gFly
    https://www.gFly.dev
    All rights reserved.

### Usage

Install
```bash
go get -u github.com/gflydev/event@v1.0.0
```


## Core Concepts

### IEvent

An event is a plain Go struct carrying data about something that occurred. It must implement `IEvent`:

```go
type IEvent interface {
    EventName() string  // unique name, convention: "domain.action"
}
```

### IListener[T]

A listener is a **generic** interface typed to one concrete event. The `Handle` method receives the concrete event directly — no type assertions needed.

```go
type IListener[T IEvent] interface {
    Handle(event T) error
}
```

### ISubscriber

A subscriber groups multiple listeners for a domain and wires them to the dispatcher:

```go
type ISubscriber interface {
    Subscribe(dispatcher *Dispatcher)
}
```

---

## Defining Events

Events and listeners for a domain live together in the same package under `internal/events/<domain>/`.

```go
// internal/events/user/user_events.go
package user

import "gfly/internal/domain/models"

const (
    EventUserRegistered = "user.registered"
    EventUserUpdated    = "user.updated"
    EventUserDeleted    = "user.deleted"
)

// UserRegistered is dispatched after a new user account is created.
type UserRegistered struct {
    User *models.User
}
func (e UserRegistered) EventName() string { return EventUserRegistered }

// UserUpdated is dispatched after a user's profile has been modified.
type UserUpdated struct {
    User *models.User
}
func (e UserUpdated) EventName() string { return EventUserUpdated }

// UserDeleted is dispatched after a user has been deleted from the system.
type UserDeleted struct {
    UserID int
    Email  string
}
func (e UserDeleted) EventName() string { return EventUserDeleted }
```

---

## Defining Listeners

Listeners live in the same package as their events. The `Handle` method receives the concrete event type directly — no type assertion needed.

### Synchronous Listener

Runs in the same goroutine as the dispatcher — blocks the caller until done.

```go
// internal/events/user/send_welcome_email_listener.go
package user

import (
    "gfly/internal/notifications"
    "github.com/gflydev/core/log"
    "github.com/gflydev/notification"
)

// SendWelcomeEmailListener sends a welcome email when a new user registers.
type SendWelcomeEmailListener struct{}

func (l *SendWelcomeEmailListener) Handle(event UserRegistered) error {
    log.Infof("[Listener] SendWelcomeEmail: sending to %s", event.User.Email)

    _ = notification.Send(notifications.SendMail{
        Email: event.User.Email,
    })

    return nil
}
```

```go
// internal/events/user/cleanup_user_data_listener.go
package user

import "github.com/gflydev/core/log"

// CleanupUserDataListener removes user-related data after account deletion.
type CleanupUserDataListener struct{}

func (l *CleanupUserDataListener) Handle(event UserDeleted) error {
    log.Infof("[Listener] CleanupUserData: cleaning up for user %d (%s)", event.UserID, event.Email)

    // TODO: Remove cached data, revoke sessions, delete uploaded files, etc.

    return nil
}
```

### Queued Listener (via queue worker)

For slow or critical operations that need persistence and retry. The listener pushes a task
to Redis; the queue worker (`./build/artisan queue:run`) processes it later.

```go
// internal/events/user/queued_welcome_email_listener.go
package user

import (
	"gfly/internal/console/queues"
	"github.com/gflydev/console"
	"github.com/gflydev/core/log"
)

// QueuedWelcomeEmailListener defers the welcome email to the queue worker.
// Requires the queue worker to be running: ./build/artisan queue:run
type QueuedWelcomeEmailListener struct{}

func (l *QueuedWelcomeEmailListener) Handle(event UserRegistered) error {
	log.Infof("[Listener] QueuedWelcomeEmail: queuing for %s", event.User.Email)

	console.DispatchTask(queues.NewSendWelcomeEmailTask(event.User.Email, event.User.Fullname))

	return nil
}
```

---

## Registering Listeners with a Subscriber

Each domain has one `Subscriber` struct that wires its events to listeners. Use `events.ListenOn[T]` — the event name is inferred automatically from `T` via reflection (safe for both value and pointer event types).

```go
// internal/events/user/user_subscriber.go
package user

import "gfly/internal/events"

// Subscriber groups all listeners for user-domain events.
type Subscriber struct{}

func (s *Subscriber) Subscribe(d *events.Dispatcher) {
    events.ListenOn[UserRegistered](d, &SendWelcomeEmailListener{})
    events.ListenOn[UserDeleted](d, &CleanupUserDataListener{})
}
```

Register the subscriber in `internal/events/init.go`:

```go
// internal/events/init.go
package listeners

import (
    "gfly/internal/events"
    "gfly/internal/events/user"
)

func init() {
    events.Subscribe(&user.Subscriber{})
    // events.Subscribe(&order.Subscriber{})  // ← add new domains here
}
```

The `init.go` is auto-loaded via the blank import in `cmd/web/main.go`:

```go
_ "gfly/internal/events" // Autoload event listeners.
```

---

## Dispatching Events

Call `events.Dispatch()` from a service after the business operation completes. Import the domain package for the event type.

### Synchronous (blocks until all listeners finish)

```go
// internal/services/user_services.go
import (
    "gfly/internal/events"
    userEvents "gfly/internal/events/user"
)

func CreateUser(createUserDto dto.CreateUser) (*models.User, error) {
    // ... create user in DB

    if err := events.Dispatch(userEvents.UserRegistered{User: user}); err != nil {
        log.Errorf("UserRegistered event error: %v", err)
    }

    return user, nil
}

func DeleteUserByID(userID int) error {
    // ... delete user from DB

    if err := events.Dispatch(userEvents.UserDeleted{UserID: user.ID, Email: user.Email}); err != nil {
        log.Errorf("UserDeleted event error: %v", err)
    }

    return nil
}
```

### Asynchronous (returns immediately, listeners run in background)

```go
// Fire event in a background goroutine — does not block the caller
events.DispatchAsync(userEvents.UserRegistered{User: user})
```

> **When to use which:**
> - `Dispatch` — when the HTTP response depends on listener results (e.g., validation, DB writes).
> - `DispatchAsync` — when listeners are side effects (emails, logs, cache warm-up).
> - Queued listener — when the work must survive a server restart or needs retry on failure.

---

## Registering Listeners Directly (without a Subscriber)

For simple one-off registrations, use the global `events.Listen[T]()`:

```go
// Inside a setup function or init()
events.Listen[userEvents.UserRegistered](&user.SendWelcomeEmailListener{})
```


---

## Comparison: Sync vs Async vs Queued

| Feature                 | `Dispatch`     | `DispatchAsync`      | Queued Listener                    |
|-------------------------|----------------|----------------------|------------------------------------|
| Blocks HTTP request     | Yes            | No                   | No                                 |
| Error propagation       | Yes            | Logged only          | Logged only                        |
| Survives server restart | No             | No                   | Yes (Redis-backed)                 |
| Retry on failure        | No             | No                   | Via queue worker                   |
| Requires queue worker   | No             | No                   | Yes (`./build/artisan queue:run`)  |


## Adding a New Domain

Follow the `user` domain as a template. Example: adding an `order` domain.

**1.** Create `internal/events/order/` with:

```
order/
  order_events.go     → OrderPlaced, OrderShipped, OrderCanceled
  order_subscriber.go → Subscriber wiring events to listeners
  send_confirmation_listener.go
  queued_fulfillment_listener.go
```

**2.** Register in `internal/events/init.go`:

```go
import (
    "gfly/internal/events"
    "gfly/internal/events/order"
    "gfly/internal/events/user"
)

func init() {
    events.Subscribe(&user.Subscriber{})
    events.Subscribe(&order.Subscriber{})
}
```

**3.** Dispatch from the service:

```go
import orderEvents "gfly/internal/events/order"

events.Dispatch(orderEvents.OrderPlaced{Order: order})
```

---

## File Checklist for a New Event

- [ ] Add event struct + constant to `internal/events/<domain>/<domain>_events.go`
- [ ] Create listener(s) in `internal/events/<domain>/<action>_listener.go`
- [ ] Register listener(s) in `internal/events/<domain>/<domain>_subscriber.go`
- [ ] Register the domain `Subscriber` in `internal/events/init.go`
- [ ] Dispatch the event from the relevant service after the operation succeeds
- [ ] (Optional) Create queue task in `internal/console/queues/` for queued processing
