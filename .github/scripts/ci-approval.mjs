export function hasCurrentApproval(reviews, headSHA, author) {
  if (!Array.isArray(reviews) || typeof headSHA !== "string" || typeof author !== "string")
    return false;
  const latestByReviewer = new Map();
  for (const review of reviews) {
    if (typeof review?.login === "string") latestByReviewer.set(review.login, review);
  }
  return [...latestByReviewer].some(
    ([login, review]) =>
      login !== author && review.state === "APPROVED" && review.commit_id === headSHA,
  );
}

if (process.env.CI_APPROVAL_CLI === "1") {
  const reviews = JSON.parse(process.env.REVIEWS_JSON);
  console.log(hasCurrentApproval(reviews, process.env.PR_HEAD_SHA, process.env.PR_AUTHOR));
}
