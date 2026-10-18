if(NOT DEFINED SIMULATOR_EXE OR NOT DEFINED OUTPUT_DIR)
    message(FATAL_ERROR "SIMULATOR_EXE and OUTPUT_DIR are required")
endif()

file(REMOVE_RECURSE "${OUTPUT_DIR}")
set(png_header_expected "89504e470d0a1a0a0000000d49484452000001900000012c")

function(run_scene scene stem variant)
    execute_process(
        COMMAND "${SIMULATOR_EXE}" --scene "${scene}" --variant "${variant}"
                --output "${OUTPUT_DIR}"
        RESULT_VARIABLE run_result
        OUTPUT_VARIABLE run_stdout
        ERROR_VARIABLE run_stderr
    )
    if(NOT run_result EQUAL 0)
        message(FATAL_ERROR
            "simulator failed for scene=${scene} variant=${variant}\n"
            "stdout:\n${run_stdout}\nstderr:\n${run_stderr}")
    endif()

    set(png "${OUTPUT_DIR}/${stem}.png")
    set(packed "${OUTPUT_DIR}/${stem}.packed")
    set(rgb565 "${OUTPUT_DIR}/${stem}.rgb565")
    set(sha_file "${OUTPUT_DIR}/${stem}.sha256")
    set(meta_file "${OUTPUT_DIR}/${stem}.json")

    foreach(artifact IN ITEMS "${png}" "${packed}" "${rgb565}" "${sha_file}" "${meta_file}")
        if(NOT EXISTS "${artifact}")
            message(FATAL_ERROR "missing simulator artifact: ${artifact}")
        endif()
    endforeach()

    file(SIZE "${packed}" packed_size)
    if(NOT packed_size EQUAL 15000)
        message(FATAL_ERROR "${packed}: expected 15000 bytes, got ${packed_size}")
    endif()

    file(SIZE "${rgb565}" rgb_size)
    if(NOT rgb_size EQUAL 240000)
        message(FATAL_ERROR "${rgb565}: expected 240000 bytes, got ${rgb_size}")
    endif()

    file(SIZE "${png}" png_size)
    if(png_size LESS 1000)
        message(FATAL_ERROR "${png}: unexpectedly small (${png_size} bytes)")
    endif()

    file(READ "${png}" png_header HEX LIMIT 24)
    string(TOLOWER "${png_header}" png_header)
    if(NOT png_header STREQUAL png_header_expected)
        message(FATAL_ERROR "${png}: invalid PNG signature or dimensions (${png_header})")
    endif()

    file(SHA256 "${packed}" packed_digest)
    file(READ "${sha_file}" recorded_digest)
    string(STRIP "${recorded_digest}" recorded_digest)
    if(NOT recorded_digest STREQUAL packed_digest)
        message(FATAL_ERROR
            "${stem}: digest file ${recorded_digest} != packed bytes ${packed_digest}")
    endif()

    file(READ "${meta_file}" metadata)
    string(FIND "${metadata}" "\"scene\": \"${scene}\"" scene_index)
    if(scene_index EQUAL -1)
        message(FATAL_ERROR "${meta_file}: scene metadata missing")
    endif()
    string(FIND "${metadata}" "\"variant\": \"${variant}\"" variant_index)
    if(variant_index EQUAL -1)
        message(FATAL_ERROR "${meta_file}: variant metadata missing")
    endif()
    string(FIND "${metadata}" "\"sha256\": \"${packed_digest}\"" digest_index)
    if(digest_index EQUAL -1)
        message(FATAL_ERROR "${meta_file}: digest metadata missing or stale")
    endif()
endfunction()

run_scene(monitor normal normal)
run_scene(monitor inverted inverted)

execute_process(
    COMMAND "${SIMULATOR_EXE}" --list-scenes
    RESULT_VARIABLE list_result
    OUTPUT_VARIABLE list_output
    ERROR_VARIABLE list_error
)
if(NOT list_result EQUAL 0 OR NOT list_output MATCHES "monitor")
    message(FATAL_ERROR "scene listing failed\n${list_output}\n${list_error}")
endif()

execute_process(
    COMMAND "${SIMULATOR_EXE}" --scene does-not-exist
    RESULT_VARIABLE unknown_result
    OUTPUT_VARIABLE unknown_output
    ERROR_VARIABLE unknown_error
)
if(unknown_result EQUAL 0)
    message(FATAL_ERROR "unknown scene unexpectedly succeeded")
endif()

message(STATUS "simulator end-to-end test passed")
