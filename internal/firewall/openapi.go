package firewall

import "cubeship/internal/platform/openapi"

// The Docker caveat is repeated in three descriptions on purpose. It is
// the one thing about a firewall on this kind of host that somebody
// integrating against it has to know, and a reference is read one
// operation at a time.
const dockerNote = "\n\n**Docker publishes ports around UFW.** A published container port is *forwarded* rather than delivered to the host, so it never passes the chain `ufw allow` and `ufw deny` govern — on this instance that is Traefik's 80 and 443, every exposed datastore, and the daemon itself. A rule with scope `host` does not touch any of them.\n\nWhat does is scope `apps`, and it only means anything once `POST /firewall/docker` has installed the stanza that routes Docker's own chain through UFW. Until then such a rule is refused rather than written, because a rule that governs nothing is worse than no rule."

func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Firewall",
			Description: "The host's UFW: whether it is on, what it admits, and whether published container ports are answerable to it at all. Admin only — the list of what is open is exactly what somebody probing would like to have." + dockerNote,
		}},
		Schemas: map[string]*openapi.Schema{
			"FirewallRule": openapi.Object(map[string]*openapi.Schema{
				"index": openapi.Integer("The position UFW gave it, and how it is deleted. It shifts whenever anything above it goes, so it is read fresh and never stored."),
				"text":  openapi.String("The rule as UFW prints it. UFW's own output is richer than the parsed fields — an interface, a rate limit — so this is what a screen should show."),
				"scope": {Type: "string", Enum: []string{"host", "apps"}, Description: "`host` is traffic to the machine; `apps` is traffic forwarded to a container, which is what UFW calls a routed rule."},
				"action": {Type: "string", Enum: []string{"allow", "deny", "reject"},
					Description: "What happens to what it matches. Absent for a rule this daemon could not parse, which is kept rather than hidden."},
				"protocol":  {Type: "string", Enum: []string{"tcp", "udp"}, Description: "Absent for a rule that covers both."},
				"ports":     openapi.String(`What it admits, as UFW spells it: "22", "80,443", "15000:15999".`),
				"from":      openapi.String("Where it applies from. Absent for anywhere."),
				"comment":   openapi.String("UFW's own comment on the rule."),
				"protected": openapi.Bool("This rule admits SSH on a running firewall, and deleting it here is refused — the same guarantee enabling keeps, from the other side. It can still be removed on the machine, where the person doing it can see what happens next; the refusal names the command."),
				"v6":        openapi.Bool("The IPv6 half of a rule UFW wrote twice. One decision, two lines — a listing that shows both reads as a firewall with twice as many rules as it has."),
			}, "index", "text", "scope", "protected", "v6"),

			"FirewallPublishedPort": openapi.Object(map[string]*openapi.Schema{
				"port":      openapi.Integer("A host port a container is answering on right now."),
				"protocol":  openapi.String("tcp or udp."),
				"container": openapi.String("Which container publishes it."),
				"allowed":   openapi.Bool("Whether a rule already admits it. Ports bound to loopback are not listed at all: nothing outside the host can reach them, and offering to open one would be offering a hole for something that was never exposed."),
			}, "port", "protocol", "container", "allowed"),

			"FirewallRuleRequest": openapi.Object(map[string]*openapi.Schema{
				"scope":    {Type: "string", Enum: []string{"host", "apps"}, Description: "`host` for the machine's own services — SSH, anything not in a container. `apps` for a published container port."},
				"action":   {Type: "string", Enum: []string{"allow", "deny", "reject"}, Description: "`deny` drops silently; `reject` answers."},
				"protocol": {Type: "string", Enum: []string{"tcp", "udp"}, Description: "Omit for both. A port range must name one — UFW cannot write a range into both tables at once."},
				"port":     openapi.String(`A port, or a range as UFW spells one: "15000:15999".`),
				"sources":  openapi.Array(openapi.String("An address or CIDR this applies from.")),
				"comment":  openapi.String("Carried into UFW's own comment, so `ufw status` on the host says who wrote it and why."),
			}, "scope", "action", "port"),

			"Firewall": openapi.Object(map[string]*openapi.Schema{
				"available":        openapi.Bool("False when this daemon cannot reach the host — it is running as a host process rather than as a container. Then nothing else here is known."),
				"installed":        openapi.Bool("False when the host has no `ufw`. Not an error: installing it is the operator's call, on their machine's package manager."),
				"enabled":          openapi.Bool("Whether UFW is active."),
				"default_incoming": openapi.String("What happens to traffic no rule matches — `deny` or `allow`. The most important fact here and the one people assume rather than check."),
				"rules":            openapi.Array(openapi.Ref("FirewallRule")),
				"docker_adopted":   openapi.Bool("Whether published container ports are answerable to UFW at all — see the note on this tag. False means every `apps` rule would be inert, so writing one is refused."),
				"ssh_ports":        openapi.Array(openapi.Integer("A port the host's sshd said it is listening on. **Absent means it did not say**, and is not filled in with 22 — a guess is harmless while it only decides whether to refuse, and is a lockout once it decides which port to open.")),
				"ssh_allowed":      openapi.Bool("Whether some rule admits one of those. False is not a refusal: enabling writes the rule itself, from the port above."),
				"your_ip":          openapi.String("The address this request came from, so a screen can offer \"just me\" without sending somebody to look their own address up. What the daemon sees — the forwarded address through Traefik, the socket's otherwise."),
				"published":        openapi.Array(openapi.Ref("FirewallPublishedPort")),
			}, "available", "installed", "enabled", "rules", "docker_adopted", "ssh_allowed", "published"),
		},
		Paths: map[string]openapi.PathItem{
			"/firewall": {
				"get": {
					OperationID: "getFirewall",
					Summary:     "Read the host's firewall",
					Description: "Everything this instance can say about it in one call: whether UFW is installed and on, every rule, which ports containers publish, and whether those are governed at all." + dockerNote,
					Tags:        []string{"Firewall"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall as it stands.", openapi.Ref("Firewall")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			"/firewall/enable": {
				"post": {
					OperationID: "enableFirewall",
					Summary:     "Turn the firewall on",
					Description: "**It admits SSH first if nothing else does.** UFW denies incoming by default, so enabling it on a machine reached over SSH with no rule for SSH ends that session and every future one — silently, and to somebody who is by then unable to undo it. The port comes from the host's own sshd, so the rule is the right one, and it is written before the firewall comes up.\n\nThe one refusal left is a host whose sshd did not say which port it is on: any rule written there would be a guess, and a wrong guess is the lockout. Add the rule for the port you connect on, then ask again.\n\nEnabling does not close a published container port: see the note on this tag.",
					Tags:        []string{"Firewall"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, now on.", openapi.Ref("Firewall")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("The host did not say which port sshd is on, ufw is not installed, or this daemon cannot reach the host."),
					},
				},
			},
			"/firewall/disable": {
				"post": {
					OperationID: "disableFirewall",
					Summary:     "Turn the firewall off",
					Description: "No refusal attached: switching a firewall off cannot lock anybody out, and an operator who wants it off in a hurry is usually having a bad enough day.",
					Tags:        []string{"Firewall"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, now off.", openapi.Ref("Firewall")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
			"/firewall/rules": {
				"post": {
					OperationID: "addFirewallRule",
					Summary:     "Add a rule",
					Description: "Scope decides which of two different things this is.\n\n`sources` is a list because UFW takes one source per rule — there is no \"from A or B\" — so admitting a port from three addresses is three rules. This is the one request that writes them: every one is checked before any is written, because a request half applied is a firewall nobody asked for. An empty list, or any empty entry, means anywhere — and anywhere makes everything narrower meaningless, so it wins." + dockerNote,
					Tags:        []string{"Firewall"},
					RequestBody: openapi.Body(openapi.Ref("FirewallRuleRequest")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, with the rule in it.", openapi.Ref("Firewall")),
						"400": openapi.TextResponse("The rule does not describe anything: an unknown scope or action, a port that is not a number or a range, a source that is not an address."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("An `apps` rule while Docker's ports are not adopted, ufw is not installed, or this daemon cannot reach the host."),
					},
				},
			},
			"/firewall/rules/{index}": {
				"put": {
					OperationID: "replaceFirewallRule",
					Summary:     "Edit a rule",
					Description: "UFW has no edit, so this is a delete and an add — and the order is the point. The new rule goes in **first**, at the old one's position, and the old one goes after: the other way round has a window where the rule is simply gone, and an add that then fails leaves a firewall missing a line nobody took out on purpose. This way the window holds a duplicate, which is harmless.\n\nThe position is kept because order is meaning in a firewall: the first rule that matches decides, so a rule appended to the end may now be shadowed by one above it. With the firewall off there are no positions and the rules are appended instead.\n\nRefused for the rule admitting SSH, for the same reason deleting it is: changing its source is a lockout with an extra step. Pass `expect` as you would when deleting.",
					Tags:        []string{"Firewall"},
					Parameters: []openapi.Parameter{
						openapi.PathParam("index", "The rule's position, from the listing."),
						openapi.QueryParam("expect", "The rule's `text` as you last read it."),
					},
					RequestBody: openapi.Body(openapi.Ref("FirewallRuleRequest")),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, with the rule replaced.", openapi.Ref("Firewall")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("There is no rule at that position."),
						"409": openapi.TextResponse("The list moved, the rule is the one admitting SSH, or an `apps` rule while Docker's ports are not adopted."),
					},
				},
				"delete": {
					OperationID: "deleteFirewallRule",
					Summary:     "Delete a rule",
					Description: "UFW deletes by position, and a position shifts as soon as anything above it goes. So pass `expect` — the rule's own `text` — and the daemon refuses if the list has moved underneath you. Without it, a screen acting on a stale listing deletes a different rule, and the only sign is a port that stops answering.",
					Tags:        []string{"Firewall"},
					Parameters: []openapi.Parameter{
						openapi.PathParam("index", "The rule's position, from the listing."),
						openapi.QueryParam("expect", "The rule's `text` as you last read it. Optional, and worth sending."),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, without it.", openapi.Ref("Firewall")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("There is no rule at that position."),
						"409": openapi.TextResponse("That position holds something other than what you were looking at, or the rule is the one admitting SSH on a running firewall."),
					},
				},
			},
			"/firewall/docker": {
				"post": {
					OperationID: "adoptDockerPorts",
					Summary:     "Put published container ports under UFW",
					Description: "Installs a stanza in the host's `/etc/ufw/after.rules` that routes Docker's `DOCKER-USER` chain through UFW's forward chain — the one seam Docker leaves and never rewrites. After this, an `apps` rule governs a published port, and a published port with no rule is closed.\n\n**That last part is why this takes a list.** Everything currently published and not allowed goes dark the moment the stanza lands, so `allow_ports` says what to keep — read `published` from `GET /firewall` to see what that would be. Ports 80 and 443 are allowed whatever the list says: they are Traefik, which is every app on the instance and the dashboard this was pressed from.\n\nThe allow rules are written first, while they are still inert, and the stanza that starts denying goes last. Idempotent: a host that already has it is left alone.",
					Tags:        []string{"Firewall"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"allow_ports": openapi.Array(openapi.Integer("A published port to keep reachable.")),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, now governing published ports.", openapi.Ref("Firewall")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("ufw is not installed, or this daemon cannot reach the host."),
					},
				},
				"delete": {
					OperationID: "releaseDockerPorts",
					Summary:     "Take published container ports back out of UFW",
					Description: "Removes the stanza. Published ports go back to being reachable regardless of any rule — which is Docker's own behaviour, and the reason the stanza exists. Only the text between Cubeship's markers is removed; anything an operator wrote in that file by hand stays.",
					Tags:        []string{"Firewall"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The firewall, no longer governing published ports.", openapi.Ref("Firewall")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
			},
		},
	}
}
