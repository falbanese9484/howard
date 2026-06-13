package tui

type history struct {
	limit   int
	cursor  int
	prompts []string
}

func historyInit(limit int) *history {
	return &history{
		limit:   limit,
		cursor:  0,
		prompts: make([]string, 0, limit),
	}
}

func (c *history) Push(prompt string) {
	if c.limit == 0 {
		return
	}

	c.prompts = append(c.prompts, prompt)
	if len(c.prompts) > c.limit {
		c.prompts = c.prompts[1:]
	}

	c.cursor = len(c.prompts)
}

func (c *history) Prev() string {
	if len(c.prompts) == 0 {
		return ""
	}

	if c.cursor > 0 {
		c.cursor--
	}

	return c.prompts[c.cursor]
}

func (c *history) Next() string {
	if len(c.prompts) == 0 || c.cursor >= len(c.prompts) {
		return ""
	}

	if c.cursor == len(c.prompts)-1 {
		c.cursor = len(c.prompts)
		return ""
	}

	c.cursor++

	return c.prompts[c.cursor]
}
