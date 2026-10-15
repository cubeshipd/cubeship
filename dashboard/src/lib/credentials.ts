import { BoxIcon, type LucideIcon } from "lucide-react";
import type { ComponentType } from "react";
import { AWSIcon, CloudflareIcon, DigitalOceanIcon } from "@/components/icons";

// A mark per provider, and nothing else.
//
// What a provider is called and what its fields are called come from
// the daemon — `GET /dns/providers` — because those are decided by
// which clients the release actually has, and a copy here would be a
// copy that drifts. A logo is the one thing the daemon has no business
// shipping.
export const PROVIDER_ICONS: Record<string, ComponentType<{ className?: string }> | LucideIcon> = {
  aws: AWSIcon,
  cloudflare: CloudflareIcon,
  digitalocean: DigitalOceanIcon,
};

export function providerIcon(provider: string) {
  return PROVIDER_ICONS[provider] ?? BoxIcon;
}
