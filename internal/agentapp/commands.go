package agentapp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ExecuteSlashCommand parses and handles one slash command.
func ExecuteSlashCommand(content string, env CommandEnv) tea.Cmd {
	if env.Session == nil {
		appendError(env, "session is not initialized")
		return nil
	}

	parts := strings.Fields(strings.TrimSpace(content))
	if len(parts) == 0 {
		return nil
	}
	command := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	switch command {
	case "help":
		appendAssistant(env, strings.Join([]string{
			"Slash commands:",
			"/help",
			"/session",
			"/name <display-name>",
			"/new",
			"/resume [session-id|latest]",
			"/tree [entry-id]",
			"/branch <entry-id>",
			"/fork <entry-id>",
			"/compact [keep_messages]",
			"/queue",
			"/dequeue",
		}, "\n"))
	case "session":
		stats := env.Session.Stats()
		appendAssistant(env, fmt.Sprintf(
			"session=%s name=%q leaf=%s entries=%d user=%d assistant=%d tool_calls=%d tool_results=%d queued=(steer:%d follow_up:%d)",
			stats.SessionID,
			stats.SessionName,
			stats.LeafID,
			stats.EntryCount,
			stats.UserMessages,
			stats.AssistantMsgs,
			stats.ToolCalls,
			stats.ToolResults,
			stats.SteeringQueued,
			stats.FollowUpQueued,
		))
	case "name":
		if len(args) == 0 {
			name := strings.TrimSpace(env.Session.SessionName())
			if name == "" {
				appendAssistant(env, "Session name is empty. Use /name <display-name>.")
			} else {
				appendAssistant(env, fmt.Sprintf("Session name: %q", name))
			}
			return nil
		}
		name := strings.TrimSpace(strings.Join(args, " "))
		if name == "-" {
			name = ""
		}
		if err := env.Session.SetSessionName(context.Background(), name); err != nil {
			appendError(env, err.Error())
			return nil
		}
		if name == "" {
			appendAssistant(env, "Session name cleared.")
		} else {
			appendAssistant(env, fmt.Sprintf("Session name set to %q.", name))
		}
	case "new":
		if env.ActiveStream {
			appendError(env, "cannot create new session while agent is running")
			return nil
		}
		id, err := env.Session.NewSession(context.Background(), "")
		if err != nil {
			appendError(env, err.Error())
			return nil
		}
		rebuildChat(env)
		refreshStatus(env)
		appendAssistant(env, "Started new session "+id+".")
	case "resume":
		if env.ActiveStream {
			appendError(env, "cannot resume session while agent is running")
			return nil
		}
		if len(args) == 0 {
			if env.OpenResumeSelector == nil {
				appendError(env, "resume selector is not available")
				return nil
			}
			return env.OpenResumeSelector()
		}

		targetID := strings.TrimSpace(args[0])
		if strings.EqualFold(targetID, "latest") {
			infos, err := env.Session.ListSessions(context.Background())
			if err != nil {
				appendError(env, err.Error())
				return nil
			}
			if len(infos) == 0 {
				appendAssistant(env, "No sessions found.")
				return nil
			}
			current := env.Session.SessionID()
			targetID = infos[0].ID
			for _, info := range infos {
				if info.ID != current {
					targetID = info.ID
					break
				}
			}
		}
		if err := env.Session.SwitchSession(context.Background(), targetID); err != nil {
			appendError(env, err.Error())
			return nil
		}
		rebuildChat(env)
		refreshStatus(env)
		appendAssistant(env, "Resumed session "+targetID+".")
	case "tree":
		if env.ActiveStream {
			appendError(env, "cannot switch branch while agent is running")
			return nil
		}
		if len(args) == 0 {
			if env.OpenTreeSelector == nil {
				appendError(env, "tree selector is not available")
				return nil
			}
			return env.OpenTreeSelector()
		}
		if len(args) != 1 {
			appendError(env, "usage: /tree [entry-id]")
			return nil
		}
		if err := env.Session.SwitchBranch(args[0]); err != nil {
			appendError(env, err.Error())
			return nil
		}
		rebuildChat(env)
		appendAssistant(env, "Switched branch to "+args[0]+".")
	case "branch", "fork":
		if env.ActiveStream {
			appendError(env, "cannot switch branch while agent is running")
			return nil
		}
		if len(args) != 1 {
			appendError(env, "usage: /branch <entry-id>")
			return nil
		}
		if err := env.Session.SwitchBranch(args[0]); err != nil {
			appendError(env, err.Error())
			return nil
		}
		rebuildChat(env)
		appendAssistant(env, "Switched branch to "+args[0]+".")
	case "compact":
		if env.ActiveStream {
			appendError(env, "cannot compact while agent is running")
			return nil
		}
		keep := 0
		if len(args) > 0 {
			parsed, err := strconv.Atoi(args[0])
			if err != nil {
				appendError(env, "usage: /compact [keep_messages]")
				return nil
			}
			keep = parsed
		}
		result, err := env.Session.Compact(context.Background(), keep, "")
		if err != nil {
			appendError(env, err.Error())
			return nil
		}
		rebuildChat(env)
		appendAssistant(env, fmt.Sprintf("Compaction completed. Dropped %d messages.", result.DroppedMessages))
	case "queue":
		steering := env.Session.SteeringQueued()
		followUp := env.Session.FollowUpQueued()
		if len(steering) == 0 && len(followUp) == 0 {
			appendAssistant(env, "No queued messages.")
			return nil
		}
		lines := make([]string, 0, len(steering)+len(followUp)+2)
		lines = append(lines, "Queued messages:")
		for _, message := range steering {
			lines = append(lines, "- steer: "+message)
		}
		for _, message := range followUp {
			lines = append(lines, "- follow-up: "+message)
		}
		appendAssistant(env, strings.Join(lines, "\n"))
	case "dequeue":
		steering, followUp := env.Session.ClearQueue()
		all := append(append([]string(nil), steering...), followUp...)
		if len(all) == 0 {
			appendAssistant(env, "No queued messages to restore.")
			return nil
		}
		prefix := strings.Join(all, "\n\n")
		current := strings.TrimSpace(getInputValue(env))
		if current != "" {
			prefix = prefix + "\n\n" + current
		}
		setInputValue(env, prefix)
		appendAssistant(env, fmt.Sprintf("Restored %d queued messages to input.", len(all)))
	default:
		appendError(env, "unknown slash command: /"+command)
	}

	return nil
}

func appendAssistant(env CommandEnv, text string) {
	if env.AppendAssistant != nil {
		env.AppendAssistant(text)
	}
}

func appendError(env CommandEnv, errText string) {
	if env.AppendError != nil {
		env.AppendError(errText)
	}
}

func rebuildChat(env CommandEnv) {
	if env.RebuildChatFromSession != nil {
		env.RebuildChatFromSession()
	}
}

func refreshStatus(env CommandEnv) {
	if env.RefreshSessionStatus != nil {
		env.RefreshSessionStatus()
	}
}

func getInputValue(env CommandEnv) string {
	if env.GetInputValue == nil {
		return ""
	}
	return env.GetInputValue()
}

func setInputValue(env CommandEnv, value string) {
	if env.SetInputValue != nil {
		env.SetInputValue(value)
	}
}
