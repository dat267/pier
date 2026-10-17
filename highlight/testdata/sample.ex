defmodule Demo do
  def greet(name) do
    IO.puts("Hello, #{name}!")
  end

  defp helper(x), do: x + 1
end

x = 42
s = ~s(hello)
r = ~r/abc/i
atom = :ok
map = %{a: 1}
