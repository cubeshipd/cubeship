import { z } from "zod";
import { manifestSchema } from "./schema";

// io: "input" describes what an author writes rather than what the parser
// produces, which is the difference between a document that validates
// their file and one that validates ours.
export function templateJsonSchema(): object {
  return {
    ...z.toJSONSchema(manifestSchema, { io: "input", unrepresentable: "any" }),
    $id: "https://cubeship.dev/schema/template/v1.json",
    title: "Cubeship template, version 1",
  };
}
