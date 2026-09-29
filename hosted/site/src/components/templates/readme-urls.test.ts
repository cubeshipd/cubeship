import { describe, expect, it } from "vitest";
import { resolveReadmeUrl } from "./readme-urls";

const where = { owner: "cubeshipd", repo: "cubeship-umami-template", commit: "abc" };

describe("resolveReadmeUrl", () => {
  it("pins a relative image to the template's commit", () => {
    expect(resolveReadmeUrl("./docs/shot.png", "image", where)).toBe(
      "https://raw.githubusercontent.com/cubeshipd/cubeship-templates/abc/cubeship-umami-template/docs/shot.png",
    );
  });

  it("sends a relative link to the file on GitHub", () => {
    expect(resolveReadmeUrl("LICENSE", "link", where)).toBe(
      "https://github.com/cubeshipd/cubeship-templates/blob/abc/cubeship-umami-template/LICENSE",
    );
  });

  it("leaves absolute URLs and anchors alone", () => {
    expect(resolveReadmeUrl("https://umami.is", "link", where)).toBe("https://umami.is");
    expect(resolveReadmeUrl("#install", "link", where)).toBe("#install");
    expect(resolveReadmeUrl("mailto:a@b.c", "link", where)).toBe("mailto:a@b.c");
  });
});
