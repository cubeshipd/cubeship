# RabbitMQ on Cubeship

[RabbitMQ](https://www.rabbitmq.com) is a message broker: apps publish messages
to queues and other apps consume them, over AMQP, MQTT or STOMP.

This template installs it on a Cubeship instance, with its management UI on a
domain, AMQP on a TCP port, and its queues kept in a volume.

## What it creates

- **rabbitmq** — RabbitMQ with the management plugin, from
  `rabbitmq:4.3.5-management`. The management UI answers on the domain you
  choose; queues, users and definitions are kept in a volume at
  `/var/lib/rabbitmq`.

It needs Cubeship 0.7.2 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the management UI answers | A domain you control, pointed at your instance. |
| The username apps and the UI sign in with | Anything; `admin` unless you change it. |
| That user's password | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| The port AMQP answers on | `5672` by default. Choose a port from `1024` to `65535`, and open it in your provider's firewall too. |

## Connecting an app

AMQP is not on the domain: the domain carries HTTP only. From outside the
instance, connect to the instance's address on the port you chose. From an app
on the same instance, use RabbitMQ's internal address, shown on its page in the
dashboard. Installed with the suggested names, that is:

```
amqp://<username>:<password>@cubeship-rabbitmq-production-rabbitmq:5672
```

For an external client, the equivalent URL is
`amqp://<username>:<password>@<your VPS address>:<chosen port>`. AMQP has no
TLS configured here, so do not publish it to an untrusted network without
putting an authenticated, encrypted proxy in front of it.

## After installing

1. Open the domain and sign in with the username and the generated password.
2. Create a user per app under *Admin → Users*, rather than sharing this one.

The username and password only create the first user. Changing the variables
afterwards changes nothing.

## The volume

The broker runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds: clients reconnect. Messages in durable queues survive
it; messages in transient queues do not. Back the volume up from the app's
settings.

## Resources

The app is limited to 1 CPU and 1 GiB of memory, and RabbitMQ stops accepting
messages when it nears that limit. Raise `limits` in `template.yaml` for
long queues.
