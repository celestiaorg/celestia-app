<!--
order: 1
-->

# Concepts

## The Minting Mechanism

The minting mechanism was designed to allow for a strict inflation rate planned ahead of time.

The initial parameters are determined as follows:

| Initial Inflation |     8.00% |
|------------------------|----------------------------|
| Disinflation rate p.a |    10.00% |
| Target Inflation (floor) |  1.50% |

The above params are fixed and will not changed. So we hardcoded them.
It reduces the inflation per year until it reaches the target inflation.

**Example:** if we stick to `10%` decrease per year, we will have something like this:

| year | inflation (%) |
|------|------|
|0  | 8.00 |
|1  | 7.20 |
|2  | 6.48 |
|3  | 5.832 |
|4  | 5.2488 |
|5  | 4.72392 |
|6  | 4.251528 |
|7  | 3.8263752 |
|8  | 3.44373768 |
|9  | 3.099363912 |
|10 | 2.7894275208 |
|11 | 2.51048476872 |
|12 | 2.259436291848 |
|13 | 2.0334926626632 |
|14 | 1.83014339639688 |
|15 | 1.647129056757192 |
|16 | 1.50 |
|17 | 1.50 |
|18 | 1.50 |
|19 | 1.50 |
|20 | 1.50 |
