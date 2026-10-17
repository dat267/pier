// a comment
functions {
  real square(real x) {
    return x * x;
  }
}

data {
  int<lower=0> N;
  vector[N] y;
}

parameters {
  real mu;
  real<lower=0> sigma;
}

model {
  target += normal_lpdf(y | mu, sigma);
  mu ~ normal(0, 10);
  # include a comment
}
