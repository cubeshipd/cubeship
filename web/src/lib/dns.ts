import type { ComponentType } from "react";
import { AWSIcon, CloudflareIcon } from "@/components/icons";
import type { DNSProvider } from "@/lib/api";

// What each provider is called, marked with, and asked for.
//
// The two are asked different things, and the difference is not
// cosmetic: Cloudflare's credential is one API token, and Route 53's is
// an access key, which is two halves. `userLabel` is empty where there
// is no named half, and that emptiness is what every form here reads to
// decide whether to show the field at all.
//
// It lives in lib rather than beside a page because three screens need
// it and none of them owns it.
export const DNS_PROVIDERS: Record<
  DNSProvider,
  {
    label: string;
    icon: ComponentType<{ className?: string }>;
    hint: string;
    userLabel: string;
    secretLabel: string;
  }
> = {
  cloudflare: {
    label: "Cloudflare",
    icon: CloudflareIcon,
    userLabel: "",
    secretLabel: "API token",
    hint: "A scoped API token, not the global key. Create one under My Profile → API Tokens with Zone:DNS:Edit on the zones Cubeship should manage.",
  },
  route53: {
    label: "AWS Route 53",
    icon: AWSIcon,
    userLabel: "Access key ID",
    secretLabel: "Secret access key",
    hint: "An IAM access key. Route 53 is global — there is no region to pick — and the key needs route53:ListHostedZones and ChangeResourceRecordSets.",
  },
};

// The record types Cubeship offers. Not everything both providers
// support — that is a long list including several nobody sets by hand —
// but what someone pointing a name at this host, or proving they own it,
// actually needs. It mirrors dns.RecordTypes in the daemon, which is
// what refuses anything else.
export const RECORD_TYPES = ["A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA"];
