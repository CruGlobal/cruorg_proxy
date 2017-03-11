#!/usr/bin/ruby -w

push = ARGV.delete('--push') || ARGV.delete('-p')

unless ARGV.empty?
  puts "Usage: build.rb [--push]"
  puts "Builds this project, and pushes it to ecr if --push (or -p) argument is present"
  exit 1
end


def run(command, failure_message)
  puts command
  system command
  if ($? != 0)
    puts failure_message
    exit 1
  end
end

puts 'running build'

run 'main/build.sh', 'build failed'

puts 'building test container'
#run 'test/build.sh', 'building test container failed'


#puts 'running cruorg_proxy'


# clean up any failed container from a previous run.
# Normally docker cleans up after itself when we use --rm,
# but under some circumstances (like an invalid --link) it doesn't.
#output = `docker rm cruorg_proxy-container 2>&1`
#if $?.to_i > 0 && output.index("no such id") == nil
#  puts "unable to remove cruorg_proxy-container"
#  puts output
#  exit 1
#end

#pid = spawn({'NO_TTY' => 'true'}, 'main/run.sh')

#begin
#  sleep 8 # hopefully that's enough...

#  puts 'running test'
#  run 'test/run_test_locally.sh', 'running test failed'
#  puts 'test finished successfully'

#ensure
#  puts "shutting down cruorg_proxy (#{pid})"

#  `kill #{pid}`

#  Process.wait(pid)

#end

git_commit = ENV['GIT_COMMIT'] || `git rev-parse --verify HEAD`.strip
build_number = ENV['BUILD_NUMBER'] || 0

name = "056154071827.dkr.ecr.us-east-1.amazonaws.com/cruorg_proxy:#{git_commit}-#{build_number}"
run "docker tag 056154071827.dkr.ecr.us-east-1.amazonaws.com/cruorg_proxy #{name}", 'tag failed'

if push
  run "docker push #{name}", 'push failed'
end

