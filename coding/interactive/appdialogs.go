package interactive

import "github.com/dat267/pier/coding"

// The port's mid-session dialogs. Upstream reaches these through its extension
// UI, whose steps are promises the caller awaits; the port's equivalent is the
// selector slot plus a callback, so the seams that ask a question take an answer
// function rather than returning a value (the slot's Show never blocks).

// askChoose shows a titled list and reports the chosen option, or "" when the
// dialog is dismissed.
func (a *App) askChoose(title string, message string, options []string, onSelect func(option string)) {
	a.slot.Show(func(done func()) CreatedSelector {
		selector := NewExtensionSelectorComponent(title, options,
			func(option string) {
				done()
				if onSelect != nil {
					onSelect(option)
				}
			},
			func() {
				done()
				if onSelect != nil {
					onSelect("")
				}
			},
			ExtensionSelectorOptions{Description: message, Tui: a.ui},
		)
		return CreatedSelector{Component: selector, Focus: selector}
	})
}

// askConfirm asks a yes/no question (upstream's extension-UI confirm) and treats
// a dismissal as "no".
func (a *App) askConfirm(title string, message string, onAnswer func(confirmed bool)) {
	a.askChoose(title, message, []string{"Yes", "No"}, func(option string) {
		if onAnswer != nil {
			onAnswer(option == "Yes")
		}
	})
}

// askMissingSessionCwd offers the ways to open a session whose stored cwd is
// gone (upstream promptForMissingSessionCwd): continue in the fallback
// directory once, fork into it, or cancel.
func (a *App) askMissingSessionCwd(issue coding.SessionCwdIssue, onAnswer func(choice coding.MissingSessionCwdChoice)) {
	options := coding.MissingSessionCwdOptions(issue)
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = option.Label
	}
	a.askChoose("Session cwd not found", coding.FormatMissingSessionCwdPrompt(issue), labels, func(option string) {
		if onAnswer == nil {
			return
		}
		for _, candidate := range options {
			if candidate.Label == option {
				onAnswer(candidate.Choice)
				return
			}
		}
		onAnswer(coding.MissingSessionCwdCancel)
	})
}
