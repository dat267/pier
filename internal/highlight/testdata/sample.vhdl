library ieee;
use ieee.std_logic_1164.all;

entity counter is
  port (
    clk : in std_logic;
    rst : in std_logic;
    q   : out std_logic_vector(7 downto 0)
  );
end entity;

architecture rtl of counter is
  signal count : integer := 0;
begin
  process (clk, rst) begin
    if rst = '1' then
      count <= 0;
    elsif rising_edge(clk) then
      count <= count + 1;
    end if;
  end process;
end architecture;
