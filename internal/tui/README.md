# Howard's TUI

The TUI is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and follows the usual Elm-style loop: `Init`, `Update`, and `View`. The root model in `model.go` owns the application state, delegates rendering/state to smaller components, and bridges user input to the chat bus.

## Structure
- `model.go`: root Bubble Tea model, message handling, layout, response streaming, and keybindings
- `components/input.go`: prompt textarea configuration
- `components/chatview.go`: scrollable transcript viewport
- `components/selector.go`: model picker shown by `Ctrl+F`
- `clipboard.go`: small in-memory prompt history used by up/down navigation
- `logo.go`: startup logo/tagline content
- `styles/`: shared Lip Gloss styles

## Runtime Flow
1. `cmd/tui` loads config, initializes the provider and chat bus, then creates `tui.Model`.
2. `Model.Init` starts textarea blinking.
3. `Model.Update` receives Bubble Tea messages, updates the input and viewport, and dispatches resize, key, provider response, and model-selection events.
4. On `Enter`, the current prompt is pushed into history, sent through the chat bus, and the model waits for streamed provider responses.
5. Response chunks are rendered through Glamour as markdown. Tool status messages are committed into the transcript separately.

## Modes
- Chat mode is the default. The view shows the transcript, optional thinking box, and prompt input.
- Model selection mode is entered with `Ctrl+F`. It temporarily replaces the chat view with a selectable list of provider models.

## Keybindings
- `Enter`: send the current prompt
- `Ctrl+F`: open model selector
- `Esc` or `Ctrl+C`: close the selector, or quit from chat mode
- `Up` / `Down`: navigate recent prompts in chat mode; move selection in model selector mode
- `exit`: typed as a prompt, quits the TUI

## Layout Notes
The main content width is two thirds of the terminal width. `updateLayout` recalculates viewport and input dimensions on resize and reserves extra vertical space when the thinking box is visible.

Keep layout changes centralized there when possible; it makes resize behavior easier to reason about.
