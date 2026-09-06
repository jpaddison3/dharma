package cli

import (
	"io"
	"strings"
)

// selectTaskTextField chooses between a typed command's plain-text and HTML
// inputs. Presence is passed separately from value because an explicitly empty
// plain value can be meaningful (set-notes uses it to clear a description).
// HTML @-references are expanded only after conflicts have been rejected.
func selectTaskTextField(
	plainField, htmlField, plainValue, htmlValue string,
	plainPresent, htmlPresent, required bool,
	warnings io.Writer,
) (field, value string, present bool, err error) {
	plainFlag := "--" + strings.ReplaceAll(plainField, "_", "-")
	htmlFlag := "--" + strings.ReplaceAll(htmlField, "_", "-")

	if plainPresent && htmlPresent {
		return "", "", false, usageErrorf("%s and %s are mutually exclusive", plainFlag, htmlFlag)
	}
	if !plainPresent && !htmlPresent {
		if required {
			return "", "", false, usageErrorf("provide %s or %s", plainFlag, htmlFlag)
		}
		return "", "", false, nil
	}
	if plainPresent {
		return plainField, plainValue, true, nil
	}

	resolved, err := expandAtValue(htmlValue)
	if err != nil {
		return "", "", false, err
	}
	if resolved == "" {
		return "", "", false, usageErrorf("%s is empty; use <body></body> for empty rich text", htmlFlag)
	}
	warnAPINumericCharacterReferences(map[string]string{htmlField: resolved}, warnings)
	return htmlField, resolved, true, nil
}

// buildTaskCreateBody builds the description/assignee fields for task create.
// Project/workspace placement is added by the caller, which may need an API
// call to discover the workspace.
func buildTaskCreateBody(
	name, notes, htmlNotes, assignee string,
	notesPresent, htmlNotesPresent bool,
	warnings io.Writer,
) (map[string]interface{}, error) {
	body := map[string]interface{}{"name": name}
	field, value, present, err := selectTaskTextField(
		"notes", "html_notes", notes, htmlNotes,
		notesPresent, htmlNotesPresent, false, warnings,
	)
	if err != nil {
		return nil, err
	}
	// Preserve task create's existing contract: an empty plain description is
	// omitted, including when --notes "" was explicitly supplied.
	if present && !(field == "notes" && value == "") {
		body[field] = value
	}
	if assignee != "" {
		body["assignee"] = assignee
	}
	return body, nil
}
