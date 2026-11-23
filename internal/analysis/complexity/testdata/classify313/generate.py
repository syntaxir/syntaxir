# Random search generator for #313: left-recursive bodies of rule l, drawn from a fixed seed.
# usage: python3 generate.py <seed> <count> <outdir>
import random, sys, os
seed, count, out = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
rnd = random.Random(seed)
LIT = ['"a"', '"b"', '"+{"', '"q"', '"ab"', '"x"', '"{"', '"+"', '"}"', '""']
REF = ['k', 'w', 'z', 'l']
def atom(d):
    r = rnd.random()
    if d < 2 and r < 0.2:
        return "(" + choice(d + 1) + ")"
    if r < 0.55:
        return rnd.choice(LIT)
    return rnd.choice(REF)
def item(d):
    a = atom(d)
    r = rnd.random()
    if r < 0.12: a += "?"
    elif r < 0.22: a += "*"
    elif r < 0.27: a = "&" + a
    elif r < 0.32: a = "!" + a
    return a
def seq(d):
    return " ".join(item(d) for _ in range(rnd.randint(1, 4)))
def choice(d):
    return " / ".join(seq(d) for _ in range(rnd.randint(1, 3)))
def body():
    # left recursion: at least one alternative starts with l; a base alternative (no l first) keeps
    # the rule matchable, so that the lint does not reject it as never matching.
    alts = ["l " + seq(1) for _ in range(rnd.randint(1, 3))]
    alts += [seq(1) for _ in range(rnd.randint(1, 3))]
    alts.append(rnd.choice(["k", "z", "w", '"a"', "(z? z*)"]))
    rnd.shuffle(alts)
    return " / ".join(alts)
os.makedirs(out, exist_ok=True)
for i in range(count):
    src = (
        "@start\n"
        'rule s = (l / "+{" / "}")* $$\n'
        "rule l = " + body() + "\n"
        'rule k = "a"\n'
        "rule w = z?\n"
        'rule z = "b" "+" "{" [ab+{]* "}" "!"\n'
    )
    with open(os.path.join(out, "c%04d.sxir" % i), "w") as f:
        f.write(src)
