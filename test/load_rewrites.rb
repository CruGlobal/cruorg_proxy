require 'redis'
redis = Redis.new(:port => 6379, :db => 3, host: 'redis-stage.t5vw3x.0001.use1.cache.amazonaws.com')

File.readlines('./static_rewrites.txt').each do |line|
  key, val = line.split(' ')
  redis.set("redirect:#{key}", val)
  puts line
end

File.readlines('./regex_rewrites.txt').each do |line|
  key, val = line.split(' ')
  redis.hset('redirects:regex', key, val)
  puts line
end

