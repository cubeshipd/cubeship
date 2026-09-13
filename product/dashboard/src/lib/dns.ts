// The record types Cubeship offers. Not everything both providers
// support — that is a long list including several nobody sets by hand —
// but what someone pointing a name at this host, or proving they own it,
// actually needs. It mirrors dns.RecordTypes in the daemon, which is
// what refuses anything else.
export const RECORD_TYPES = ["A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA"];
