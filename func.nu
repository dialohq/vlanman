print "Uninstalling vm"
helm list -o json | from json | where name =~ "vm" | get name | each {|it| helm uninstall $it}
print "Waiting"
loop { let exists = (kubectl get ns -o json | from json | get items.metadata.name | where $it =~ "vlanman-system" | length); if $exists == 0 { print "done"; break } else { print "..."; sleep 0.5sec } }
print "Installing"
helm install vm ./helm

print "Waiting for controller"
loop {let running = (kubectl get pods -n vlanman-system -o json | from json | get items | where $it.metadata.name =~ vm-vlanman | get status.phase | where $it =~ "Running" | length); if $running == 1 { print "Running"; break;} else {print "..."}}
print "Done"
