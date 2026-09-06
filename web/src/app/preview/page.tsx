"use client";

// TEMPORARY verification harness. Removed before the work lands.
import { useEffect, useState } from "react";
import Credentials from "@/app/(dashboard)/credentials/page";
import DNSProviders from "@/app/(dashboard)/dns/page";
import RegistrySettings from "@/app/(dashboard)/registries/[id]/settings/page";
import Registries from "@/app/(dashboard)/registries/page";

const DATA: Record<string, unknown> = {
  "/credentials": [
    {
      id: 1,
      provider: "aws",
      provider_name: "AWS",
      label: "acme production",
      username: "AKIAIOSFODNN7EXAMPLE",
      capabilities: ["dns", "registry"],
      in_use_by: ["DNS provider", "123456789012.dkr.ecr.us-east-1.amazonaws.com"],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: 2,
      provider: "cloudflare",
      provider_name: "Cloudflare",
      label: "personal",
      capabilities: ["dns"],
      in_use_by: [],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: 3,
      provider: "digitalocean",
      provider_name: "DigitalOcean",
      label: "do token",
      capabilities: ["registry"],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  "/credentials/providers": [
    {
      provider: "aws",
      name: "AWS",
      capabilities: ["dns", "registry"],
      username_label: "Access key ID",
      password_label: "Secret access key",
      hint: "An IAM access key. Route 53 and ECR both.",
    },
    {
      provider: "cloudflare",
      name: "Cloudflare",
      capabilities: ["dns"],
      password_label: "API token",
      hint: "A token with Zone:Read and DNS:Edit.",
    },
    {
      provider: "digitalocean",
      name: "DigitalOcean",
      capabilities: ["registry"],
      password_label: "API token",
      hint: "A personal access token with registry scope.",
    },
    {
      provider: "generic",
      name: "Other",
      capabilities: ["registry"],
      username_label: "Username",
      password_label: "Password or token",
      hint: "Anything that takes a username and a password.",
    },
  ],
  "/registries": [
    {
      id: 7,
      credential_id: 1,
      provider: "aws",
      host: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
      region: "us-east-1",
      username: "AKIAIOSFODNN7EXAMPLE",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: 8,
      credential_id: 3,
      provider: "digitalocean",
      host: "registry.digitalocean.com",
      namespace: "acme",
      username: "do token",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  "/settings": { registry_host: "registry.example.com" },
};

function answer(path: string): unknown {
  if (path.startsWith("/credentials?capability=dns")) {
    return (DATA["/credentials"] as { capabilities: string[] }[]).filter((c) =>
      c.capabilities.includes("dns"),
    );
  }
  if (path.startsWith("/credentials?capability=registry")) {
    return (DATA["/credentials"] as { capabilities: string[] }[]).filter((c) =>
      c.capabilities.includes("registry"),
    );
  }
  if (/^\/(dns|registries)\/\d+\/status$/.test(path)) return { state: "available" };
  return DATA[path] ?? [];
}

const PARAMS = Promise.resolve({ id: "7" });
const SEARCH = Promise.resolve({});

export default function Preview() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    window.fetch = (async (input: RequestInfo | URL) => {
      const path = String(input).replace(/^.*\/api/, "");
      return new Response(JSON.stringify(answer(path)), {
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;
    setReady(true);
  }, []);
  if (!ready) return null;
  return (
    <div className="mx-auto max-w-5xl space-y-16 p-8">
      <Credentials />
      <DNSProviders />
      <Registries />
      <RegistrySettings params={PARAMS} searchParams={SEARCH} />
    </div>
  );
}
