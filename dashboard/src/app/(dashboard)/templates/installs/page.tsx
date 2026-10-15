import { redirect } from "next/navigation";

// The breadcrumb over an installation names this address, and the list
// of installations is a tab of the Templates screen.
export default function TemplateInstalls() {
  redirect("/templates?tab=installed");
}
