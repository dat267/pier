module counter (
    input  wire clk,
    input  wire rst,
    output reg [7:0] q
);
    always @(posedge clk) begin
        if (rst)
            q <= 8'h00;
        else
            q <= q + 8'd1;
    end
endmodule
