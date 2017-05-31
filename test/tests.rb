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
redis.hset('redirects:regex', "/level(.*)", '/level1/level%1.html')
tests = {}
tests["/level(.*)"] = [location(`curl -k -s --head https://localhost/level2?purge_vanity`), '/level1/level2.html']
run_tests(tests)
redis.hdel('redirects:regex', "/level(.*)")

# with caching the previous test should work even with the redis key deleted
tests = {}
tests["/level(.*) - target cache"] = [location(`curl -k -s --head https://localhost/level2`), '/level1/level2.html']
run_tests(tests)

# with purge_vanity parameter, it should return a 404
tests = {}
tests["/level(.*) - cache purge"] = [location(`curl -k -s --head https://localhost/level2?purge_vanity`), 'no-cache']
run_tests(tests)

redis.hset('redirects:regex', "^/campus/(.*)", '/communities/campus/%1')
tests = {}
tests["/campus/(.*)"] = [location(`curl -k -s --head https://localhost/communities/campus/foo?purge_vanity`), 'no-cache']
run_tests(tests)
redis.hdel('redirects:regex', "^/campus/(.*)")

puts "\n\n"

puts "UPSTREAM tests"
# Test the default route
tests = {}
tests['Default route'] = [`curl -k -s https://localhost`.strip, 'index']
run_tests(tests)

puts "\n"

# Test level1 matching - an upstream value for /level1 should match everything under it
puts "Level 1 tests"
redis.set('upstreams:/level1', 'WP_ADDR')
tests = {}
tests['/level1.html'] = [`curl -k -s https://localhost/level1.html?purge_target`.strip, 'level1']
tests['/level1/level2.html'] = [`curl -k -s https://localhost/level1/level2.html?purge_target`.strip, 'level2']
tests['/level1/level2/level3.html'] = [`curl -k -s https://localhost/level1/level2/level3.html?purge_target`.strip, 'level3']
tests['/level1/level2/level3/level4.html'] = [`curl -k -s https://localhost/level1/level2/level3/level4.html?purge_target`.strip, 'level4']
run_tests(tests)

# Test level2 matching - an upstream value for /level1/level2 should match everything under level2, but not level1
puts "\nLevel 2 tests"
redis.del('upstreams:/level1')
redis.set('upstreams:/level1/level2', 'WP_ADDR')
tests = {}
tests['/level1.html'] = [`curl -k -s https://localhost/level1.html?purge_target`.strip, '404']
tests['/level1/level2.html'] = [`curl -k -s https://localhost/level1/level2.html?purge_target`.strip, 'level2']
tests['/level1/level2/level3.html'] = [`curl -k -s https://localhost/level1/level2/level3.html?purge_target`.strip, 'level3']
tests['/level1/level2/level3/level4.html'] = [`curl -k -s https://localhost/level1/level2/level3/level4.html?purge_target`.strip, 'level4']
run_tests(tests)

# Test level3 matching - an upstream value for /level1/level2/level3 should match everything under level3, but not 1 or 2
puts "\nLevel 3 tests"
redis.del('upstreams:/level1/level2')
redis.set('upstreams:/level1/level2/level3', 'WP_ADDR')
tests = {}
tests['/level1.html'] = [`curl -k -s https://localhost/level1.html?purge_target`.strip, '404']
tests['/level1/level2.html'] = [`curl -k -s https://localhost/level1/level2.html?purge_target`.strip, '404']
tests['/level1/level2/level3.html'] = [`curl -k -s https://localhost/level1/level2/level3.html?purge_target`.strip, 'level3']
tests['/level1/level2/level3/level4.html'] = [`curl -k -s https://localhost/level1/level2/level3/level4.html?purge_target`.strip, 'level4']
run_tests(tests)
redis.del('upstreams:/level1/level2/level3')


