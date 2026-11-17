package security

import "regexp"

var localSiteImageReference = regexp.MustCompile(`^/media/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func IsLocalSiteImageReference(value string) bool { return localSiteImageReference.MatchString(value) }
