package app

import "cubeship/internal/platform/openapi"

// deployHookDoc is how the webhook itself is called. It is described here
// rather than as a path of its own: it lives at the root, outside /api,
// where every server URL in this document ends — so no operation in it
// could name the address a CI actually posts to.
const deployHookDoc = "\n\n**Calling it.** `POST <url>` with no credentials — the token is the whole authorization, so treat the URL as a secret. Only POST: a GET would deploy whenever a link preview fetched it. Parameters go in the query string, and only there: a body, of any content type, is read and ignored, so GitHub, Docker Hub or any other sender can post its own payload to the URL. A parameter the webhook does not take is `400`, naming it:\n\n" +
	"- `tag` — for an app that runs a published image. On an external app, or one on this instance's registry pinned to a tag, the tag is **saved** on the app, as if it were edited on the settings screen, so the next deploy from anywhere stays on it. On a registry app that follows pushes it is deployed without being saved, so autodeploy stays on.\n" +
	"- `ref` — for an app that builds: the branch, tag or commit for this one deploy. Not saved.\n" +
	"- `wait` — `true` answers once the deploy has finished, with `200`, instead of `202` as soon as it is accepted. A failed deploy is still `200`: read `status`. The wait is capped at 15 minutes: a deploy still running then is answered `202` with its current `status`, and carries on. Read as a boolean (`true`, `1`, `false`, `0`, …); anything else is `400`.\n\n" +
	"The answer is `{id, status, app, tag | ref, error}`. A tag sent to an app that builds, a ref sent to one that does not, or either malformed, is `400`; a ref holds no spaces, `:`, `#`, `,`, `..` or `@{`, and does not start with a dash. A token that names no app is a plain `404`, the same as any address nothing is served at. Each call is in the audit log, by `deploy webhook`, via `webhook`."

const deployHookRole = "\n\nRequires the member role, and the admin role for an app that **builds** — the same as deploying it, because holding the URL is being able to. An app changed to a source that builds gets a new token with that change, so a URL read under the member role stops working."

func deployHookPaths(refParams []openapi.Parameter, appPath string) map[string]openapi.PathItem {
	responses := openapi.Responses{
		"200": openapi.JSONResponse("The app's webhook.", openapi.Ref("DeployHook")),
		"401": openapi.Unauthorized,
		"403": openapi.Forbidden,
		"404": openapi.NotFound,
	}
	return map[string]openapi.PathItem{
		appPath + "/deploy-hook": {
			"get": {
				OperationID: "getDeployHook",
				Summary:     "Read an app's deploy webhook",
				Description: "Every app has one, from the moment it is created: a URL that deploys it when something posts to it, for a CI that pushes to a registry Cubeship does not run and so has no other way to say there is a new image." + deployHookDoc + deployHookRole,
				Tags:        []string{"Apps"},
				Parameters:  refParams,
				Responses:   responses,
			},
		},
		appPath + "/deploy-hook/rotate": {
			"post": {
				OperationID: "rotateDeployHook",
				Summary:     "Regenerate an app's deploy webhook",
				Description: "Gives the webhook a new token. **The old URL stops working at once**, so every CI that used it has to be given the new one." + deployHookRole,
				Tags:        []string{"Apps"},
				Parameters:  refParams,
				Responses:   responses,
			},
		},
	}
}

func deployHookSchemas() map[string]*openapi.Schema {
	url := openapi.String("`https://<instance domain>/hooks/deploy/<token>`. Null while the instance has no domain: there is no public address to give yet.")
	url.Nullable = true
	return map[string]*openapi.Schema{
		"DeployHook": openapi.Object(map[string]*openapi.Schema{
			"url":   url,
			"token": openapi.String("The secret in the URL: 64 hexadecimal characters."),
		}, "url", "token"),
	}
}
