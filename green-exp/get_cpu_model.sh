for node in node-0 node-1 node-2 node-3; do
  echo "==== $node ===="
  ssh "$node" "lscpu | egrep 'Model name|Socket|Core|Thread|CPU\(s\)'"
done