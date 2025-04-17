// SPDX-License-Identifier: LGPL-3.0-only

pragma solidity ^0.8.25;

contract CancunOpcodes {
    /// @notice TSTORE + TLOAD using fixed slot (0x42)
    /// @param value The value to store in transient storage
    /// @return loadedValue Value loaded from transient slot 0x42
    function testTstoreTload(uint256 value) external returns (uint256 loadedValue) {
        assembly {
            tstore(0x42, value)
            loadedValue := tload(0x42)
        }
    }

    /// @notice Store 1-byte value in memory, copy it via MCOPY, then load and return
    /// @param value The 1-byte value to test
    /// @return copied 32-byte memory word after MCOPY
    function testSimpleMCopy(uint8 value) external pure returns (bytes32 copied) {
        assembly {
            let dst := mload(0x40)
            mstore8(dst, value) // 정확히 dst 위치에 1 byte만 기록
            mstore(0x40, add(dst, 0x40))
            mcopy(add(dst, 0x20), dst, 0x20) // mcopy 이후 32바이트 읽기 위해 32 바이트 오프셋으로 mload
            copied := mload(add(dst, 0x20))
        }
    }

    /// @notice Returns the current blob base fee
    /// @return fee Blob base fee (expected 0 on most networks)
    function testBlobBaseFee() external view returns (uint256 fee) {
        assembly {
            fee := blobbasefee()
        }
    }

    /// @notice Returns the blob hash for a given index
    /// @param index Blob index to query
    /// @return hash 32-byte blob commitment hash
    function testBlobHash(uint256 index) external view returns (bytes32 hash) {
        assembly {
            hash := blobhash(index)
        }
    }
}
