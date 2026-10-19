package operations

// cancellationStatusNotes explains what each cancelSystemJob status means for the caller. Only these
// statuses are lifted into the envelope; anything else is left in result for the caller to inspect.
var cancellationStatusNotes = map[string]string{
	"cancelled":             "The cloud confirmed the cancellation. Work already in flight may still have finished, and completed changes are not undone.",
	"cancellationRequested": "Cancellation was requested but is not confirmed yet. Repeat the cancel for the same job to reconcile it; do not report the job as cancelled until the status is cancelled.",
	"alreadyFinished":       "The job had already ended, so nothing was cancelled. Its recorded status was corrected; check the job's status for how it ended.",
}

// cancellationStatus returns the status the backend reported for a cancelSystemJob call, or "" when the
// response does not carry a recognized one.
func cancellationStatus(result any) string {
	root, _ := result.(map[string]any)
	data, _ := root["data"].(map[string]any)
	cancel, _ := data["cancelSystemJob"].(map[string]any)
	status, _ := cancel["status"].(string)
	if _, known := cancellationStatusNotes[status]; !known {
		return ""
	}
	return status
}
