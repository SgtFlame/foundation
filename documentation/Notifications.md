# Notifications

One place where every app sends what a person must see, such as a mention, a review waiting, new mail or an agent's question (forge `plans/026e-1-Notifications.md`). This part is the data and the `notify` path. The surface (the workbench bell, its panel, toasts and browser notifications) is 26e-2.

## Built in forge 26e-1

**The entity type.** A notification is a record of the tenant's `notification` entity type (`notifications/notification.json`, owner `tenant`). Its fields are `recipient` (a user id as text), `app`, `kind`, `title`, `body`, `link` (a `workbench:` link), `key`, `read` and `created`. It has the index `by_recipient` over `recipient` and `created`.

Its own access rules allow read, update and delete only to the recipient (`string(record.recipient) == principal.user`), and allow create to no person. A type's own rule is the only one that restricts tenant Administrators, so not even they read someone else's notifications. Service principals skip the rules, and only the tenant's service principal makes notifications.

`notifications.Ensure` imports and publishes the type in a tenant the first time it is needed. It uses fixed command ids, so when several processes ensure at once, the streams drop the copies.

**Notify.** `notifications.Notify(args, deps, Notification{Recipients, App, Kind, Title, Body, Link, Key})` works like this:
- It checks that the sender holds `notification.create`. The new resource type `notification` has one action, `create`, of class `create`, marked `Apps` so an app's principal holds it in its tenant with no grant.
- It makes one record per distinct recipient, as the tenant's service principal.
- The record id is `n` followed by the first 24 hex digits of the SHA-256 of the recipient and the key (`RecordID`). A key such as `<app>:<subject>:<kind>` therefore notifies each recipient once, so a retried job or a second process makes nothing new. Without a key, each call makes new records.
- `App` defaults to the sender app's id.

The node *Notify* (`notify.send`) is allowed in `server.action`, `app.trigger`, `ui.handler` and `function` graphs. It takes `recipients` (a user id or a list), `title`, `body`, `link` and `key`, and gives the record `ids`. A sandboxed run (`bus.DryOf`) notifies no one. It is not offered in command graphs: there, notify from a trigger on the record a command writes.

**Reading.**
- `notifications.mine {unread?, limit?, cursor?}` is `records.find` over the caller's own notifications, newest first, 50 per page.
- `notifications.count {}` is `records.aggregate`, counting the caller's unread notifications as `{groups: [{key: null, values: {count}}]}`, with no groups when there are none.

Both delegate to the records queries with their params fixed to the caller, so they stay current the same way, and a principal that is not a user is refused. Marking one read is `record.update {entity: notification, set: {read: true}}`, and dismissing is `record.delete`, both as the recipient. The live call `notifications.readAll` (`MarkAllRead`) marks every unread one read and ends with `done {count}`.

**Wiring.** `serve.go` calls `notifications.ContributeAccess` and `notifications.Contribute(registry, func() notifications.Deps { return recordDeps })`. `notifications.Deps` is `records/data.Deps`.

**Tests.** `go test ./features/notifications` checks:
- two recipients are notified once each, and a retry with the same key makes nothing;
- each person reads only their own notifications, and counts follow reads;
- nobody else can mark a notification read or make one by hand;
- *Mark all read* touches only the caller's;
- an anonymous principal cannot notify.

**Not built yet.** The sources that turn Requirements mentions and Mail's review queue into notifications (Forge graphs, with 26e-2). Retention (purging read notifications after the `notifications` section's `keepDays`). The check that an app notifies only people who may read the subject.

## Built in forge 26e-2

The notification centre, in `frontend/common/src/notifications/`:
- **The bell** (`NotificationBell.tsx`) sits in the workbench's side bar, above the Workspaces menu. Its badge is `notifications.count`, and it opens a panel of the caller's latest 30 notifications (`notifications.mine`), unread ones marked with a dot. Opening one follows its `workbench:` link through the subject openers (`openWorkbenchLink`) and marks it read (`record.update` on the notification, as its recipient). *Mark all read* calls `notifications.readAll`.
- **New notifications** arriving while the page is open show a toast with an *Open* button. While the tab is hidden, they show as a browser notification instead, once the person has allowed it from the panel's *Notify me while this tab is hidden*. Those waiting when the page opens are not toasted.
- The logic (`notificationText.ts`: what is new, how long ago, reading the count) is tested by `yarn test`.

Not built yet: a per-person setting for which kinds notify, the Requirements and Mail sources, and Forge's *Notification list* and *Inbox view* prefabs.
