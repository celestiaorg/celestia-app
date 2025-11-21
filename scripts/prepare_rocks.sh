sudo apt remove -y 'librocksdb*'
sudo rm -rf /usr/local/include/rocksdb
sudo rm -f /usr/local/lib/librocksdb*

sudo apt install -y build-essential cmake git   libsnappy-dev zlib1g-dev libbz2-dev liblz4-dev libzstd-dev libgoogle-glog-dev
sudo git clone https://github.com/facebook/rocksdb.git
sudo git checkout v9.10.0
sudo mkdir build & cd build
sudo cmake ..   -DCMAKE_BUILD_TYPE=Release   -DCMAKE_INSTALL_PREFIX=/usr/local   -DROCKSDB_BUILD_SHARED=ON   -DROCKSDB_BUILD_STATIC=OFF   -DCMAKE_POSITION_INDEPENDENT_CODE=ON   -DWITH_SNAPPY=ON   -DWITH_LZ4=ON   -DWITH_ZSTD=ON   -DWITH_BZ2=ON
sudo make -j"$(nproc)"
make install
ldconfig

export CGO_CFLAGS="-I/usr/local/include"
export CGO_LDFLAGS="-L/usr/local/lib -lrocksdb -lstdc++ -lm -lz -lsnappy -llz4 -lzstd -lbz2"