package RecoverOOM

import (
	"fmt"
	"strings"
)

func LinkToGCP(ev event) string {
	if !ev.JsonPayload.InvolvedObject.Namespace.Valid {
		return fmt.Sprintf(
			"https://console.cloud.google.com/kubernetes/%s/%s/%s/%s?project=%s",
			strings.ToLower(ev.JsonPayload.InvolvedObject.Kind.StringVal),
			ev.Resource.Labels.Location,
			ev.Resource.Labels.ClusterName,
			ev.JsonPayload.InvolvedObject.Name,
			ev.Resource.Labels.ProjectID,
		)
	}

	return fmt.Sprintf(
		"https://console.cloud.google.com/kubernetes/%s/%s/%s/%s/%s?project=%s",
		strings.ToLower(ev.JsonPayload.InvolvedObject.Kind.StringVal),
		ev.Resource.Labels.Location,
		ev.Resource.Labels.ClusterName,
		ev.JsonPayload.InvolvedObject.Namespace,
		ev.JsonPayload.InvolvedObject.Name,
		ev.Resource.Labels.ProjectID,
	)
}
