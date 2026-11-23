import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CUBESHIP,
  DOCKER_HUB,
  entryFor,
  entryForTyped,
  hostOf,
  PUBLIC_IMAGE,
} from "./image-registry.ts";

const connected = ["ghcr.io", DOCKER_HUB];

test("an image names its registry the way Docker reads it", () => {
  assert.equal(hostOf("nginx"), DOCKER_HUB);
  assert.equal(hostOf("acme/api"), DOCKER_HUB);
  assert.equal(hostOf("docker.io/library/nginx"), DOCKER_HUB);
  assert.equal(hostOf("GHCR.io/acme/api"), "ghcr.io");
  assert.equal(hostOf("localhost:5000/api"), "localhost:5000");
  assert.equal(hostOf("quay.io/org/app"), "quay.io");
});

test("a registry with no login of its own is the public entry", () => {
  assert.equal(entryFor("quay.io", []), PUBLIC_IMAGE);
  assert.equal(entryFor("quay.io", connected), PUBLIC_IMAGE);
  assert.equal(entryFor("ghcr.io", connected), "ghcr.io");
  assert.equal(entryFor("ghcr.io", []), PUBLIC_IMAGE);
  assert.equal(entryFor(DOCKER_HUB, []), DOCKER_HUB);
  assert.equal(entryFor(CUBESHIP, connected), CUBESHIP);
});

test("typing a reference moves the select only for an explicit registry", () => {
  assert.equal(entryForTyped("ghcr.io/acme/api", DOCKER_HUB, connected), "ghcr.io");
  assert.equal(entryForTyped("ghcr.io/acme/api", DOCKER_HUB, []), PUBLIC_IMAGE);
  assert.equal(entryForTyped("quay.io/org/app", "ghcr.io", connected), PUBLIC_IMAGE);
  assert.equal(entryForTyped("docker.io/library/nginx", PUBLIC_IMAGE, connected), DOCKER_HUB);
  // Not yet a registry: leave the select where it is.
  assert.equal(entryForTyped("quay", PUBLIC_IMAGE, connected), PUBLIC_IMAGE);
  assert.equal(entryForTyped("nginx", PUBLIC_IMAGE, connected), PUBLIC_IMAGE);
  assert.equal(entryForTyped("acme/api", "ghcr.io", connected), "ghcr.io");
  assert.equal(entryForTyped("", DOCKER_HUB, connected), DOCKER_HUB);
});
