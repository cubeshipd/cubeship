package user

import "cubeship/internal/platform/openapi"

// OpenAPI describes this module's documented endpoints. It sits beside
// Routes on purpose: adding one without the other fails the parity test
// in internal/server.
//
// Managing your own API keys is absent by design — those routes are
// registered with HandleInternal. You create and rotate keys once, from
// the CLI or an MCP client; nobody integrates against them, and listing
// them in a public reference only invites someone to try.
func (h *Handler) OpenAPI() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Identity",
			Description: "Who the API key you are calling with belongs to, and who else may reach this instance.",
		}},
		Schemas: map[string]*openapi.Schema{
			"WhoAmI": openapi.Object(map[string]*openapi.Schema{
				"username":     openapi.String("The account this API key belongs to."),
				"role":         openapi.String("Either `admin` or `member`."),
				"has_password": openapi.Bool("Whether this account can sign in without an API key. It matters where a key is revoked: revoking the last one is allowed, and this is what says whether that leaves a way in."),
				"theme":        openapi.String("Which palette this person sees the dashboard in. Absent for the default."),
				"themes":       openapi.Array(openapi.String("A palette's name.")),
				"display_name": openapi.String("What the person is called, which a username often is not. Absent when unset."),
				"email":        openapi.String("Somewhere to reach whoever holds the account. **Nothing on this instance sends mail**; it is stored so an operator can tell whose account is whose, and it is never a second way to sign in."),
				"avatar":       openapi.String("Which of the faces in `avatars` this account wears. Absent for none."),
				"avatars":      openapi.Array(openapi.String("A face's name. Served rather than compiled into the dashboard, for the reason `themes` is: the daemon is what refuses a name.")),
			}, "username", "role", "has_password"),
			"User": openapi.Object(map[string]*openapi.Schema{
				"username":     openapi.String("The account."),
				"role":         openapi.String("Either `admin` or `member`."),
				"theme":        openapi.String("Which palette this person sees the dashboard in. Absent for the default."),
				"display_name": openapi.String("What the person is called. Absent when unset."),
				"email":        openapi.String("Somewhere to reach them. Absent when unset."),
				"avatar":       openapi.String("Which face the account wears."),
				"blocked_at":   openapi.String("RFC 3339, when the account was shut out. **Absent while it is not**, which is what makes this one field rather than a flag and a date.\n\nA blocked account keeps everything it had — its password, its keys, its sessions — and is refused at the door instead, so unblocking puts somebody back exactly where they were."),
				"created_at":   openapi.String("RFC 3339."),
			}, "username", "role", "created_at"),
			"Users": openapi.Object(map[string]*openapi.Schema{
				"users": openapi.Array(openapi.Ref("User")),
			}, "users"),
			"RevokedCredentials": openapi.Object(map[string]*openapi.Schema{
				"api_keys": openapi.Integer("How many keys were revoked."),
				"sessions": openapi.Integer("How many sessions were ended."),
			}, "api_keys", "sessions"),
			"ResetPassword": openapi.Object(map[string]*openapi.Schema{
				"username": openapi.String("The account it belongs to."),
				"password": openapi.String("The new password, shown exactly once. This instance keeps only its hash."),
			}, "username", "password"),
			"NewUser": openapi.Object(map[string]*openapi.Schema{
				"username": openapi.String("The account that was created."),
				"role":     openapi.String("Either `admin` or `member`."),
				"password": openapi.String("The password it signs in with, shown exactly once. Generated unless the request named one; this instance keeps only its hash, so there is nothing to read it back from."),
			}, "username", "role", "password"),
		},
		Paths: map[string]openapi.PathItem{
			"/users": {
				"get": {
					OperationID: "listUsers",
					Summary:     "List the accounts on this instance",
					Description: "Who can reach this instance at all. There is one instance and no tenant boundary, so this is the whole roster.\n\nAdmin only. Never returns a credential of any kind: the password is shown once, when the account is created, and keys are their owner's business.",
					Tags:        []string{"Identity"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The accounts.", openapi.Ref("Users")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
					},
				},
				"post": {
					OperationID: "createUser",
					Summary:     "Create an account",
					Description: "Creates an account and returns the password it signs in with, **shown exactly once**. There is no second endpoint that reveals it — this instance keeps only the hash — so an account whose password is lost before it is handed over is deleted and made again.\n\n**A password rather than an API key**, because a key is what a CLI or an MCP client carries and the dashboard wants a session: an account handed a key and no password could not sign in anywhere it was given the address of, and nothing here lets somebody set a first one. Keys are self-service, made by whoever wants one.\n\nAdmin only. An account is a way into this instance, so handing out the ability to mint them would hand out the instance.",
					Tags:        []string{"Identity"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"username": openapi.String("Lowercase letters, digits and dashes. Also the account's docker login user."),
						"role":     openapi.String("`admin` or `member`. Defaults to `member`."),
						"password": openapi.String("Optional, and generated when it is not given — a field somebody has to fill in is a field somebody fills in badly. At least 12 characters."),
					}, "username")),
					Responses: openapi.Responses{
						"201": openapi.JSONResponse("The account and its password.", openapi.Ref("NewUser")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"409": openapi.TextResponse("That username is already taken."),
					},
				},
			},
			"/users/{username}": {
				"patch": {
					OperationID: "updateUser",
					Summary:     "Change what an account may do, or shut it out",
					Description: "The two decisions an admin makes about somebody else's account. What is the account holder's own — their name, their face, their palette — is on `PATCH /users/me` instead, which takes no username for that reason: a preference somebody else can change is not a preference.\n\n**Blocking is the reversible half of deleting.** Nothing is revoked: the account keeps its password, its keys and its sessions, and every one of them is refused at the door while the block stands. So letting somebody back in is one request and they sign in with what they already had — where deleting and creating again would have cost them every key on every machine they own.\n\nEach field left out is left alone. Admin only, and three things are refused: your own role, blocking the account you are signed in as, and either one applied to the only admin on the instance.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("username", "The account to change.")},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"role":    openapi.String("`admin` or `member`. An admin also builds source on this host and configures the instance."),
						"blocked": openapi.Bool("Whether the account is shut out of every way in. `false` lets it back."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The account as it now is.", openapi.Ref("User")),
						"400": openapi.BadRequest,
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such account."),
						"409": openapi.TextResponse("Your own role, the account you are signed in as, or the only admin on the instance."),
					},
				},
				"delete": {
					OperationID: "deleteUser",
					Summary:     "Delete an account",
					Description: "What a person leaving looks like: the account goes, and with it every API key and every session it holds — in one transaction, so nothing that authenticates outlives the account it belonged to.\n\nAdmin only. Deleting the account you are signed in as is refused, and so is deleting the only admin: an instance with no admin can never configure itself again, and nothing here could put one back.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("username", "The account to delete.")},
					Responses: openapi.Responses{
						"204": openapi.Empty("The account and its credentials are gone."),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such account."),
						"409": openapi.TextResponse("That is the account you are signed in as, or the only admin on the instance."),
					},
				},
			},
			"/users/{username}/password": {
				"post": {
					OperationID: "resetUserPassword",
					Summary:     "Issue a new password for an account",
					Description: "Generates a password for somebody else's account and returns it **exactly once**, for an admin to hand over. This box sends no mail, so there is no reset link and no other way back in for an account that has forgotten its password — which used to mean deleting the account and making it again, losing its keys and its history to recover a secret.\n\n**The API keys are untouched**, which is the difference between this and `DELETE /users/{username}/credentials`: a forgotten password is not a lost laptop, and taking the keys as well would make the fix a morning of logging back into everything. Whoever wants both asks for both.\n\nEvery session ends, because the password changed. Admin only.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("username", "The account to issue a password for.")},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The new password.", openapi.Ref("ResetPassword")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such account."),
					},
				},
			},
			"/users/{username}/credentials": {
				"delete": {
					OperationID: "revokeUserCredentials",
					Summary:     "Revoke everything an account authenticates with",
					Description: "Ends every session and revokes every API key the account holds, and leaves the account itself. This is the answer to a laptop that walked off: what was on it stops working everywhere at once, without the account having to be deleted and made again.\n\nThe password is not touched — it is a secret in somebody's head, not a credential lying on the machine that was lost — so signing in again is how the account comes back. Admin only.",
					Tags:        []string{"Identity"},
					Parameters:  []openapi.Parameter{openapi.PathParam("username", "The account whose credentials to revoke.")},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("How much was revoked.", openapi.Ref("RevokedCredentials")),
						"401": openapi.Unauthorized,
						"403": openapi.Forbidden,
						"404": openapi.TextResponse("No such account."),
					},
				},
			},
			"/users/me": {
				"get": {
					OperationID: "whoAmI",
					Summary:     "Identify the caller",
					Description: "Reports the account the API key belongs to. `cubeship registry login` uses this to learn the username to log Docker in as — the saved credentials file only ever stores the key itself.",
					Tags:        []string{"Identity"},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The caller's identity.", openapi.Ref("WhoAmI")),
						"401": openapi.Unauthorized,
					},
				},
				"patch": {
					OperationID: "setPreferences",
					Summary:     "Change the caller's own account",
					Description: "**The caller's own, and there is no path parameter to say otherwise.** A preference somebody else can change is not a preference, and an admin renaming somebody else is not administration.\n\nEvery field is optional: one left out is left alone, and an empty string clears it — which is the only way a form can say \"no display name\".\n\n**Changing the username has a consequence worth knowing.** Sessions and API keys are held by account id, so both survive it. `docker login` is not: it sends the username alongside the key, so a push keeps being refused until you log in again under the new one.",
					Tags:        []string{"Identity"},
					RequestBody: openapi.Body(openapi.Object(map[string]*openapi.Schema{
						"theme":        openapi.String("One of the palettes in `themes` on `GET /users/me`. An empty string is the default one, which is how it is turned off rather than a second field saying so.\n\nEvery palette is dark and every one changes only colour: the layout, the type and the square corners are the product, and a theme that moved those would be a second interface to keep working."),
						"username":     openapi.String("1-32 characters of lowercase letters, digits, dot, dash or underscore, starting with a letter or a digit. It is the segment in `/users/{username}` and what `docker login` sends."),
						"display_name": openapi.String("At most 60 characters, and anything you like inside that: it is a name rather than an identifier, so nothing else about it is this instance's business."),
						"email":        openapi.String("Checked shallowly — one `@` with something either side and a dot in the domain — because the only thing that proves an address is sending to it, and nothing here sends."),
						"avatar":       openapi.String("One of the names in `avatars` on `GET /users/me`, or an empty string for none."),
					})),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The account as it now stands.", openapi.Ref("User")),
						"400": openapi.TextResponse("Nothing to change, or a value this instance refuses: an unknown theme or face, a username that cannot be one, an address that is not one."),
						"409": openapi.TextResponse("That username is taken."),
						"401": openapi.Unauthorized,
					},
				},
			},
		},
	}
}
