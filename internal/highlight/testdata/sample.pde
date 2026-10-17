int radius = 40;
float x, y;

void setup() {
  size(640, 360);
  x = width / 2.0;
  y = height / 2.0;
}

void draw() {
  background(0);
  ellipse(x, y, radius, radius);
  println(frameCount);
}
