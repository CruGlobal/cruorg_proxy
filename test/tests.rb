require 'redis'

def red(s); "\e[31m#{s}\e[0m" end

def run_tests(tests)
  tests.each do |name, test|
    if test[0] == test[1]
      puts "#{name} - PASS"
    else
      puts red("#{name} - FAIL : '#{test[0]}' != '#{test[1]}'")
    end
  end
end

def location(headers)
  headers.strip.split("\n").last.split(' ').last
end

redis = Redis.new(:port => 6380, :db => 3)
puts "REDIRECTOR TESTS"
redis.hset('redirects:regex', "'/level'{.*}", '/level1/level%1.html')
tests = {}
tests["'/level'{.*}"] = [location(`curl -s --head localhost/level2`), '/level1/level2.html']
run_tests(tests)
redis.hdel('redirects:regex', "'/level'{.*}")

redis.hset('redirects:regex', "'/level'{.*}", '/level1/level%1.html')
tests = {}
tests["^'/level'{.*}$"] = [location(`curl -s --head localhost/level2`), '/level1/level2.html']
run_tests(tests)
redis.hdel('redirects:regex', "'/level'{.*}")


puts "\n\n"

puts "UPSTREAM tests"
# Test the default route
tests = {}
tests['Default route'] = [`curl -s localhost`.strip, 'index']
run_tests(tests)

puts "\n"

# Test level1 matching - an upstream value for /level1 should match everything under it
puts "Level 1 tests"
redis.set('upstreams:/level1', 'WP_ADDR')
tests = {}
tests['/level1.html'] = [`curl -s localhost/level1.html`.strip, 'level1']
tests['/level1/level2.html'] = [`curl -s localhost/level1/level2.html`.strip, 'level2']
tests['/level1/level2/level3.html'] = [`curl -s localhost/level1/level2/level3.html`.strip, 'level3']
tests['/level1/level2/level3/level4.html'] = [`curl -s localhost/level1/level2/level3/level4.html`.strip, 'level4']
run_tests(tests)

# Test level2 matching - an upstream value for /level1/level2 should match everything under level2, but not level1
puts "\nLevel 2 tests"
redis.del('upstreams:/level1')
redis.set('upstreams:/level1/level2', 'WP_ADDR')
tests = {}
tests['/level1.html'] = [`curl -s localhost/level1.html`.strip, '404']
tests['/level1/level2.html'] = [`curl -s localhost/level1/level2.html`.strip, 'level2']
tests['/level1/level2/level3.html'] = [`curl -s localhost/level1/level2/level3.html`.strip, 'level3']
tests['/level1/level2/level3/level4.html'] = [`curl -s localhost/level1/level2/level3/level4.html`.strip, 'level4']
run_tests(tests)

# Test level3 matching - an upstream value for /level1/level2/level3 should match everything under level3, but not 1 or 2
puts "\nLevel 3 tests"
redis.del('upstreams:/level1/level2')
redis.set('upstreams:/level1/level2/level3', 'WP_ADDR')
tests = {}
tests['/level1.html'] = [`curl -s localhost/level1.html`.strip, '404']
tests['/level1/level2.html'] = [`curl -s localhost/level1/level2.html`.strip, '404']
tests['/level1/level2/level3.html'] = [`curl -s localhost/level1/level2/level3.html`.strip, 'level3']
tests['/level1/level2/level3/level4.html'] = [`curl -s localhost/level1/level2/level3/level4.html`.strip, 'level4']
run_tests(tests)

