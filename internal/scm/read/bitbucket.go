package read

import "context"

// BitbucketMirror mirrors a Bitbucket Cloud repository into mirrorPath.
func BitbucketMirror(ctx context.Context, repoURL, mirrorPath string) error {
	return Mirror(ctx, repoURL, mirrorPath)
}

// BitbucketFetch refreshes an existing Bitbucket mirror in place.
func BitbucketFetch(ctx context.Context, mirrorPath string) error {
	return Fetch(ctx, mirrorPath)
}
