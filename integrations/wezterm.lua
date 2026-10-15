-- Merge this callback into your existing config.keys. Adjust the clipctl path
-- for your installation (Intel Homebrew normally uses /usr/local/bin/clipctl).
local wezterm = require 'wezterm'
local clipctl = '/opt/homebrew/bin/clipctl'

return {
  key = 'V',
  mods = 'CMD|SHIFT',
  action = wezterm.action_callback(function(window, pane)
    -- Remote mux panes use a different pane-ID namespace. This integration is
    -- for a local macOS pane running OpenSSH, not WezTerm SSH/mux domains.
    if not pane:get_tty_name() then
      window:toast_notification('Clipport', 'Use a local pane running OpenSSH', nil, 5000)
      return
    end
    local process = pane:get_foreground_process_info()
    if not process or type(process.executable) ~= 'string' or not process.executable:match('/ssh$') then
      window:toast_notification('Clipport', 'Remote paste requires a foreground OpenSSH session', nil, 5000)
      return
    end
    local ran, ok, output, err = pcall(wezterm.run_child_process, {
      clipctl, 'paste', '--terminal', 'wezterm', '--pane-id', tostring(pane:pane_id()),
    })
    if not ran then
      window:toast_notification('Clipport', 'Cannot run clipctl: ' .. tostring(ok), nil, 5000)
      return
    end
    if not ok then
      window:toast_notification('Clipport', err, nil, 5000)
      return
    end
    -- Keep this pane object across the upload. Never look up the active pane
    -- afterward or turn errors into text pasted into the user's shell.
    if #output > 0 then
      local pasted = pcall(function()
        local current = pane:get_foreground_process_info()
        if not current or current.pid ~= process.pid then
          error('SSH session changed during the upload')
        end
        pane:send_paste(output)
      end)
      if not pasted then
        window:toast_notification('Clipport', 'The original SSH session is no longer available', nil, 5000)
      end
    end
  end),
}
