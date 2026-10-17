module ring(radius, thickness) {
    difference() {
        circle(r = radius);
        circle(r = radius - thickness);
    }
}

for (i = [0 : 10]) {
    translate([i * 10, 0, 0])
        cube([5, 5, 5], center = true);
}
include <utils.scad>
x = 1.5e-3;
