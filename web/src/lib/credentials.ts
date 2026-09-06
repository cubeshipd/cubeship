import { BoxIcon, type LucideIcon } from "lucide-react";
import type { ComponentType } from "react";
import { AWSIcon, CloudflareIcon, DigitalOceanIcon } from "@/components/icons";
import type { CredentialCapability } from "@/lib/api";

// A mark per provider, and nothing else.
//
// What a provider is called, what its fields are called and what it can
// be used for all come from the daemon — `GET /credentials/providers` —
// because those are decided by which clients the release actually has,
// and a copy here would be a copy that drifts. A logo is the one thing
// the daemon has no business shipping.
export const PROVIDER_ICONS: Record<string, ComponentType<{ className?: string }> | LucideIcon> = {
  aws: AWSIcon,
  cloudflare: CloudflareIcon,
  digitalocean: DigitalOceanIcon,
};

export function providerIcon(provider: string) {
  return PROVIDER_ICONS[provider] ?? BoxIcon;
}

// What each capability is called on screen. Short, because it appears
// as a tag beside a credential rather than as a sentence.
export const CAPABILITY_LABELS: Record<CredentialCapability, string> = {
  dns: "DNS",
  registry: "Registries",
};

// Where the DNS pages get their list. A DNS account is not a store of
// its own any more — it is a credential whose provider knows how to
// write records — so every screen that offers a choice of one asks the
// same question, spelled once here.
export const DNS_CREDENTIALS = "/credentials?capability=dns";

// The same question for registries: which stored accounts can log in to
// one.
export const REGISTRY_CREDENTIALS = "/credentials?capability=registry";
