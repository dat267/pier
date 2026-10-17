-- a line comment
--[[ a long
comment ]]
local function greet(name)
  local msg = "hello, " .. name
  local n = 0x1f + 1.5e-3
  print(msg, n, true, nil)
  return { name = name, count = n }
end
local s = [[long
string]]
greet('world')
