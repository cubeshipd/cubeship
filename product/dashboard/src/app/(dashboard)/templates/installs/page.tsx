import { redirect } from "next/navigation";

// The breadcrumb over an install names this address. An install is
// reached from the template it installed, so the catalog is where this
// goes rather than a list of its own.
export default function TemplateInstalls() {
  redirect("/templates");
}
