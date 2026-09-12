"use client";

import { usePathname } from "next/navigation";
import { useState } from "react";

export type CommentRow = {
  id: number;
  parentId: number | null;
  body: string | null;
  deleted: boolean;
  createdAt: string;
  author: { login: string; avatarUrl: string | null };
};

type Viewer = { login: string; isAdmin: boolean } | null;

function signInHref(pathname: string): string {
  return `/api/auth/github?next=${encodeURIComponent(pathname)}`;
}

async function postComment(
  slug: string,
  input: { body: string; parentId?: number },
): Promise<CommentRow> {
  const response = await fetch(`/api/v1/templates/${slug}/comments`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
  });
  const parsed = await response.json();
  if (!response.ok) throw new Error(parsed.error?.message ?? "the server refused that");
  return parsed as CommentRow;
}

function NewComment({
  onSubmit,
  placeholder,
  submitLabel,
}: {
  onSubmit: (body: string) => Promise<void>;
  placeholder: string;
  submitLabel: string;
}) {
  const [body, setBody] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function submit() {
    if (pending) return;
    setPending(true);
    setError(null);
    try {
      await onSubmit(body);
      setBody("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "the server refused that");
    } finally {
      setPending(false);
    }
  }

  return (
    <div>
      <textarea
        value={body}
        onChange={(event) => setBody(event.target.value)}
        placeholder={placeholder}
        rows={3}
        className="hud-frame w-full border border-fd-border bg-fd-background p-3 text-fd-foreground text-sm outline-none focus:border-primary"
      />
      <div className="mt-2 flex items-center gap-3">
        <button
          type="button"
          onClick={submit}
          disabled={pending || body.trim().length < 2}
          className="label border border-fd-border px-3 py-1.5 text-fd-muted-foreground hover:border-primary hover:text-primary disabled:opacity-50"
        >
          {submitLabel}
        </button>
        {error ? <p className="text-red-400 text-xs">{error}</p> : null}
      </div>
    </div>
  );
}

function CommentActions({
  comment,
  viewer,
  onEdit,
  onDelete,
}: {
  comment: CommentRow;
  viewer: Viewer;
  onEdit: (body: string) => Promise<void>;
  onDelete: () => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(comment.body ?? "");
  const [error, setError] = useState<string | null>(null);

  if (!viewer) return null;
  const isAuthor = viewer.login === comment.author.login;
  if (!isAuthor && !viewer.isAdmin) return null;

  if (editing) {
    return (
      <div className="mt-2">
        <textarea
          value={body}
          onChange={(event) => setBody(event.target.value)}
          rows={3}
          className="hud-frame w-full border border-fd-border bg-fd-background p-3 text-fd-foreground text-sm outline-none focus:border-primary"
        />
        <div className="mt-2 flex items-center gap-3">
          <button
            type="button"
            onClick={async () => {
              try {
                await onEdit(body);
                setEditing(false);
                setError(null);
              } catch (err) {
                setError(err instanceof Error ? err.message : "the server refused that");
              }
            }}
            className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-primary hover:text-primary"
          >
            Save
          </button>
          <button
            type="button"
            onClick={() => setEditing(false)}
            className="label text-fd-muted-foreground hover:text-fd-foreground"
          >
            Cancel
          </button>
          {error ? <p className="text-red-400 text-xs">{error}</p> : null}
        </div>
      </div>
    );
  }

  return (
    <div className="mt-1 flex items-center gap-3 text-xs">
      {isAuthor ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="label text-fd-muted-foreground hover:text-primary"
        >
          Edit
        </button>
      ) : null}
      <button
        type="button"
        onClick={onDelete}
        className="label text-fd-muted-foreground hover:text-red-400"
      >
        Delete
      </button>
    </div>
  );
}

function CommentBody({ comment }: { comment: CommentRow }) {
  return (
    <div>
      <p className="text-fd-muted-foreground text-xs">
        <span className="text-fd-foreground">{comment.author.login}</span>
      </p>
      {/* Plain text, as typed: no Markdown, no autolinking, whitespace kept. */}
      <p className="mt-1 whitespace-pre-wrap text-fd-foreground text-sm">
        {comment.deleted ? (
          <span className="text-fd-muted-foreground italic">deleted</span>
        ) : (
          comment.body
        )}
      </p>
    </div>
  );
}

export function Comments({
  slug,
  initialComments,
  viewer,
}: {
  slug: string;
  initialComments: CommentRow[];
  viewer: Viewer;
}) {
  const pathname = usePathname();
  const [comments, setComments] = useState(initialComments);

  const topLevel = comments.filter((comment) => comment.parentId === null);
  const repliesOf = (id: number) => comments.filter((comment) => comment.parentId === id);

  async function addTop(body: string) {
    const created = await postComment(slug, { body });
    setComments((current) => [...current, created]);
  }

  async function addReply(parentId: number, body: string) {
    const created = await postComment(slug, { body, parentId });
    setComments((current) => [...current, created]);
  }

  async function edit(id: number, body: string) {
    const response = await fetch(`/api/v1/comments/${id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ body }),
    });
    const parsed = await response.json();
    if (!response.ok) throw new Error(parsed.error?.message ?? "the server refused that");
    setComments((current) => current.map((comment) => (comment.id === id ? parsed : comment)));
  }

  async function remove(id: number) {
    const response = await fetch(`/api/v1/comments/${id}`, { method: "DELETE" });
    if (!response.ok) return;
    setComments((current) =>
      current.map((comment) =>
        comment.id === id ? { ...comment, deleted: true, body: null } : comment,
      ),
    );
  }

  return (
    <div>
      {topLevel.length === 0 ? (
        <p className="text-fd-muted-foreground text-sm">No comments yet.</p>
      ) : (
        <div className="hud-frame divide-y divide-fd-border border border-fd-border">
          {topLevel.map((comment) => (
            <div key={comment.id} className="p-4">
              <CommentBody comment={comment} />
              <CommentActions
                comment={comment}
                viewer={viewer}
                onEdit={(body) => edit(comment.id, body)}
                onDelete={() => remove(comment.id)}
              />

              {repliesOf(comment.id).length > 0 ? (
                <div className="mt-3 ml-6 space-y-3 border-fd-border border-l pl-4">
                  {repliesOf(comment.id).map((reply) => (
                    <div key={reply.id}>
                      <CommentBody comment={reply} />
                      <CommentActions
                        comment={reply}
                        viewer={viewer}
                        onEdit={(body) => edit(reply.id, body)}
                        onDelete={() => remove(reply.id)}
                      />
                    </div>
                  ))}
                </div>
              ) : null}

              {viewer ? (
                <div className="mt-3 ml-6">
                  <NewComment
                    onSubmit={(body) => addReply(comment.id, body)}
                    placeholder="Reply…"
                    submitLabel="Reply"
                  />
                </div>
              ) : null}
            </div>
          ))}
        </div>
      )}

      <div className="mt-6">
        {viewer ? (
          <NewComment
            onSubmit={addTop}
            placeholder="Say something about this template…"
            submitLabel="Comment"
          />
        ) : (
          <a href={signInHref(pathname)} className="text-primary text-sm hover:text-glow">
            Sign in to comment →
          </a>
        )}
      </div>
    </div>
  );
}
