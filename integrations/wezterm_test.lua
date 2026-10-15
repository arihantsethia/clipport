-- Run from the repository root: lua integrations/wezterm_test.lua
local output = 'quotes "$()"; हिन्दी\n\n'
local cases = {
  {name = 'success', expected = output},
  {name = 'remote mux', tty = false},
  {name = 'local shell', executable = '/bin/zsh'},
  {name = 'unknown process', missing_process = true},
  {name = 'upload error', failed = true},
  {name = 'missing clipctl', spawn_error = true},
  {name = 'ssh exited', exited = true},
  {name = 'ssh replaced', replaced = true},
  {name = 'closed pane', closed = true},
  {name = 'empty output', output = '', no_error = true},
}
for _, test in ipairs(cases) do
  local sent, notices, calls = {}, {}, 0
  local fetched = false
  local pane = {
    get_tty_name = function() if test.tty ~= false then return '/dev/ttys001' end end,
    pane_id = function() return 42 end,
    get_foreground_process_info = function()
      if test.closed and fetched then error('pane closed') end
      if test.missing_process or (test.exited and fetched) then return nil end
      return {
        pid = test.replaced and fetched and 456 or 123,
        executable = test.executable or '/usr/bin/ssh',
      }
    end,
    send_paste = function(_, value) table.insert(sent, value) end,
  }
  local window = {
    toast_notification = function(_, _, msg) table.insert(notices, msg) end,
    active_pane = function() error('must not rediscover the focused pane') end,
  }
  package.loaded.wezterm = {
    action_callback = function(fn) return fn end,
    run_child_process = function(args)
      calls = calls + 1
      if test.spawn_error then error("clipctl missing") end
      assert(args[1] == '/opt/homebrew/bin/clipctl')
      assert(args[2] == 'paste' and args[3] == '--terminal' and args[4] == 'wezterm')
      assert(args[5] == '--pane-id' and args[6] == '42')
      fetched = true -- the user may focus another pane while this runs
      return not test.failed, test.output or output, 'upload failed'
    end,
  }
  dofile('integrations/wezterm.lua').action(window, pane)
  if test.expected then
    assert(#sent == 1 and sent[1] == test.expected, test.name)
    assert(#notices == 0 and calls == 1, test.name)
  else
    assert(#sent == 0, test.name .. ': pasted unexpected output')
    assert(test.no_error or #notices == 1, test.name .. ': missing error')
  end
end
print('WezTerm integration tests passed')
