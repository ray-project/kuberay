#! /bin/bash

echo "step 1: format py files"
# See: https://github.com/google/yapf
yapf --recursive -i --style=.style.yapf  . || exit

echo "step 2: format shell files"
# See: https://github.com/koalaman/shellcheck
find . -type f \( -name "*.sh" -or -name "*.bashrc" \) -exec \
   shellcheck -x --shell=bash {} + || exit 1

echo "Step 3: cpp lint"
# bazel test --config=cpplint //... || exit 1

echo "Step 4: golang lint"
DIFF="$(gofmt -e -d .)"
if [[ -n $DIFF ]]; then
    echo "$DIFF"
    echo "please run gofmt!"
    exit 1
fi

echo "format check success~"
