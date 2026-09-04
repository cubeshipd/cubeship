"use client";

import { ExternalLinkIcon } from "lucide-react";
import type { ComponentType } from "react";
import { Button } from "@/components/ui/button";

// One button instead of four fields and a private key.
//
// GitHub takes a manifest describing the App to create, makes it, and
// redirects back with a code the daemon exchanges for the App's
// credentials. Nobody copies a PEM out of a browser, and nothing can be
// pasted into the wrong box.
//
// The manifest has to be POSTed — it is far past what a query string
// carries — but the form is built and submitted on click rather than
// rendered. This button sits inside other forms, and a <form> inside a
// <form> is not something HTML has an answer for.
export function CreateGitHubApp({
  instanceName,
  returnTo,
  label = "Create the GitHub App",
  icon: Icon,
  note = true,
}: {
  instanceName?: string;
  icon?: ComponentType<{ className?: string }>;
  // Where to send someone once the App exists. Creating it from an
  // app's settings should end back at that app, not at the page GitHub
  // happened to redirect to.
  returnTo?: string;
  label?: string;
  note?: boolean;
}) {
  function create() {
    // The origin the operator is looking at right now is the address
    // this instance is reachable on — by IP before there is a domain,
    // by name after. Deriving it is what makes this work with neither
    // configured.
    const origin = window.location.origin;
    const redirect = returnTo
      ? `${origin}/github/app-created?return=${encodeURIComponent(returnTo)}`
      : `${origin}/github/app-created`;

    const manifest = {
      // GitHub App names are unique across all of GitHub, so a bare
      // "Cubeship" would collide with the first person to try this.
      name: instanceName ? `Cubeship — ${instanceName}` : "Cubeship",
      url: origin,
      hook_attributes: { url: `${origin}/hooks/github`, active: true },
      redirect_url: redirect,
      // Where GitHub sends someone after they install it on an account.
      // With request_oauth_on_install it carries a `code` as well as the
      // installation id, and that code is what proves whose installation
      // it is — see the note on `public` below.
      setup_url: `${origin}/github/connected`,
      callback_urls: [`${origin}/github/connected`],
      setup_on_update: true,

      // Public, and it has to be.
      //
      // A private GitHub App can only be installed on the account that
      // owns it — so an App created from a personal account could only
      // ever reach that person's own repositories, and the install page
      // offered no organizations at all. Public is what makes "install
      // this on our org" possible.
      //
      // The cost is that anyone can install it, so an installation id is
      // no longer proof of anything. request_oauth_on_install is what
      // pays for that: GitHub sends the installer back through OAuth,
      // and the daemon asks GitHub which installations *that person*
      // administers before storing one. Turning this off without turning
      // off `public` would make connecting an installation a way to read
      // a stranger's private code.
      public: true,
      request_oauth_on_install: true,
      // Exactly what a build needs: read the code, and be told when it
      // changes. Nothing here can write to a repository.
      default_permissions: { contents: "read", metadata: "read" },
      default_events: ["push"],
    };

    const form = document.createElement("form");
    form.method = "post";
    form.action = "https://github.com/settings/apps/new";
    form.target = "_blank";
    form.rel = "noopener";

    const field = document.createElement("input");
    field.type = "hidden";
    field.name = "manifest";
    field.value = JSON.stringify(manifest);
    form.appendChild(field);

    // A form has to be in the document to submit, and gone afterwards:
    // leaving it would put a stale manifest in the page for as long as
    // it stayed open.
    document.body.appendChild(form);
    form.submit();
    document.body.removeChild(form);
  }

  return (
    <div className="space-y-3">
      <Button type="button" onClick={create}>
        {Icon && <Icon className="size-4 shrink-0" />}
        {label}
        <ExternalLinkIcon className="size-3.5" />
      </Button>
      {note && (
        <p className="text-xs text-muted-foreground">
          GitHub creates it from a manifest and sends the credentials back — no fields to fill in
          and no private key to copy. It asks you to confirm the name first.
        </p>
      )}
    </div>
  );
}
