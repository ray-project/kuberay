/^=== RUN/ {
    split($3, parts, "/")
    if (length(parts) > 1) {
        print
    } else {
        gsub(/===/, "---")
        print
    }
    next
}
/---/ && !/RUN/ {
    gsub(/---/, "###")
    print
    next
}
{ print }
